// A replacement validator must produce a fresh signed trail in every operator
// domain before another rolling fault can remove its independent peer. This
// bounded readiness check does not replace the campaign's full proof audit.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Reopening a driver rechecks the same recorded replacement against kernel
// identity and signed proof timestamps; it never signals or resets a baseline.
func (self *liveScenarioFaultDriver) validatorRestartProducing(ctx context.Context, state ProcessState) (bool, error) {
	if self.cfg == nil || self.cfg.Config == nil || self.cfg.Policy == nil || len(self.cfg.WalletMaterial) == 0 || state.Role != "validator" || state.StartTimeTicks == 0 {
		return false, errors.New("validator restart readiness authority is incomplete")
	}
	validatorId, err := strconv.Atoi(strings.TrimPrefix(state.ID, "validator-"))
	if err != nil || state.ID != fmt.Sprintf("validator-%d", validatorId) || validatorId < 1 || validatorId > self.cfg.Config.Topology.Validators {
		return false, errors.New("validator restart readiness identity is invalid")
	}
	started, err := time.Parse(time.RFC3339Nano, state.StartedAt)
	if err != nil || started.UnixMilli() <= 0 || started.UnixMilli() > int64(^uint64(0)>>1)-1000 {
		return false, errors.New("validator restart readiness start time is invalid")
	}
	// Supervisor timestamps historically have whole-second resolution. Require
	// the first signed hop after that entire second, not just a late old trail.
	minimumTimeMs := uint64(started.UnixMilli()) + 1000
	checkGeneration := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		ticks, err := processStartTimeTicks(state.PID)
		if err != nil || ticks != state.StartTimeTicks {
			return errors.Join(errors.New("validator replacement kernel generation changed"), err)
		}
		return nil
	}
	if err := checkGeneration(); err != nil {
		return false, err
	}
	authority, generation, err := loadScenarioPathAuthorityV2(ctx, self.cfg, self.stateDir, validatorId)
	if err != nil {
		return false, err
	}
	limits, configured, err := configuredScenarioPathProofLimits(self.cfg, validatorId)
	root := filepath.Join(self.stateDir, "runtime", state.ID, "state")
	if generation != nil {
		root = generation.ClientStateDir
		limits, err = scenarioPathProofLimitsV2(generation.Evidence.Bounds)
		configured = true
	}
	if err != nil || !configured {
		return false, errors.Join(errors.New("validator restart readiness lacks bounded proof authority"), err)
	}
	producing := true
	for noId := 1; noId <= self.cfg.Config.Topology.Operators; noId++ {
		path := filepath.Join(root, "operators", fmt.Sprintf("no-%d", noId), "proofs.jsonl")
		record, err := readRestartProofTail(ctx, path, limits)
		if err != nil {
			return false, fmt.Errorf("validator %d operator %d restart proof: %w", validatorId, noId, err)
		}
		if record == nil {
			producing = false
			continue
		}
		// Reuse the exact reviewed simulator key derivation, including its one
		// approved rotation. An endpoint cannot substitute a signer for readiness.
		_, publicKeys, err := operatorVerifyConfig(self.cfg, noId, true)
		if err != nil {
			return false, err
		}
		serverKeyKVs := make(map[byte]ed25519.PublicKey, len(publicKeys))
		for keyId, encoded := range publicKeys {
			key, err := hex.DecodeString(strings.TrimPrefix(encoded, "0x"))
			if err != nil || len(key) != ed25519.PublicKeySize {
				return false, errors.New("reviewed restart proof server key is invalid")
			}
			serverKeyKVs[keyId] = key
		}
		if err := validatorpkg.VerifyProofRecord(record, authority.keysByValidator[uint64(validatorId)][uint64(noId)], serverKeyKVs, self.cfg.Policy.Verify.TrailDepth); err != nil {
			return false, fmt.Errorf("validator %d operator %d restart proof: %w", validatorId, noId, err)
		}
		if record.Hops[0].TimeMs < minimumTimeMs {
			producing = false
		}
	}
	if err := checkGeneration(); err != nil {
		return false, err
	}
	states, _, err := self.processSnapshot()
	if err != nil {
		return false, err
	}
	current, ok := states[state.ID]
	if !ok || current.PID != state.PID || current.StartTimeTicks != state.StartTimeTicks || current.StartedAt != state.StartedAt || current.Role != state.Role || current.Identity != state.Identity {
		return false, errors.New("validator replacement changed during proof readiness")
	}
	return producing && current.Healthy, ctx.Err()
}

// Read at most two bounded rows to select the last complete proof. A concurrent
// append may leave one partial row; disappearance, truncation, replacement or
// changed complete bytes cannot authorize readiness. No historical prefix is
// trusted or rescanned by this single-proof liveness check.
func readRestartProofTail(ctx context.Context, path string, limits scenarioPathProofLimits) (*validatorpkg.ProofRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limits.maximumLine == 0 || limits.maximumLine > uint64(^uint(0)>>1)/2-1 || limits.maximumBytes == 0 {
		return nil, errors.New("restart proof tail bounds are invalid")
	}
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("restart proof source is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Size() < 0 || uint64(info.Size()) > limits.maximumBytes {
		return nil, errors.New("restart proof source exceeds its bounded regular-file owner")
	}
	size := min(uint64(info.Size()), 2*limits.maximumLine+2)
	data := make([]byte, int(size))
	offset := info.Size() - int64(size)
	if _, err := file.ReadAt(data, offset); err != nil {
		return nil, err
	}
	end := bytes.LastIndexByte(data, '\n')
	if uint64(len(data)-end-1) > limits.maximumLine {
		return nil, errors.New("partial restart proof exceeds its row bound")
	}
	if end < 0 {
		return nil, ctx.Err()
	}
	start := bytes.LastIndexByte(data[:end], '\n') + 1
	if start == 0 && offset != 0 || uint64(end-start+1) > limits.maximumLine {
		return nil, errors.New("complete restart proof exceeds its row bound")
	}
	var record validatorpkg.ProofRecord
	if err := decodeStrictJSONBytes(data[start:end], &record); err != nil {
		return nil, fmt.Errorf("decode complete restart proof: %w", err)
	}
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(info, current) || current.Size() < info.Size() {
		return nil, errors.Join(errors.New("restart proof source changed during read"), err)
	}
	confirmed := make([]byte, end-start+1)
	if _, err := file.ReadAt(confirmed, offset+int64(start)); err != nil || !bytes.Equal(confirmed, data[start:end+1]) {
		return nil, errors.Join(errors.New("restart proof row changed during read"), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &record, nil
}
