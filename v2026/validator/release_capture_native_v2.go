//go:build linux || darwin

// Final capture retains the exact native reads used by the existing source
// verifier. It neither signs nor substitutes a second native acceptance path.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
	gsrpcrpc "github.com/centrifuge/go-substrate-rpc-client/v4/rpc"
	gsrpcchain "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/chain"
	gsrpcstate "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Result and parameters are byte strings so JSON encoding cannot normalize
// an original RPC result. A null storage result remains distinct from empty.
type ReleaseEvidenceV2NativeRead struct {
	Schema     string `json:"schema"`
	Method     string `json:"method"`
	Parameters []byte `json:"parameters"`
	Result     []byte `json:"result"`
}

// One synchronous capture owns this adapter. The embedded transport belongs
// to the caller; this reader never closes it or invokes a write/subscription.
type releaseNativeCaptureClientV2 struct {
	gsrpcclient.Client
	ctx     context.Context
	maximum uint64
	retain  func(context.Context, ReleaseEvidenceV2NativeRead) error
}

// Enforces the retained-result allocation bound inside the RPC decoder.
type releaseNativeCaptureResultV2 struct {
	maximum uint64
	encoded []byte
}

// The transport's own message limits remain in force before this boundary.
func (self *releaseNativeCaptureResultV2) UnmarshalJSON(encoded []byte) error {
	if self.maximum == 0 || uint64(len(encoded)) > self.maximum || len(encoded) == 0 {
		return errors.New("native capture result exceeds its finite byte bound")
	}
	self.encoded = bytes.Clone(encoded)
	return nil
}

// Contextless convenience readers still inherit the capture lifetime.
func (self *releaseNativeCaptureClientV2) Call(result any, method string, args ...any) error {
	return self.CallContext(self.ctx, result, method, args...)
}

// Only methods used by the real historical native readers are admitted.
// Archive failure is a read failure; no caller can receive an uncaptured fact.
func (self *releaseNativeCaptureClientV2) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if self == nil || self.Client == nil || self.ctx == nil || ctx == nil || self.retain == nil || self.maximum == 0 {
		return errors.New("native capture reader is incomplete")
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	switch method {
	case "chain_getBlockHash", "chain_getHeader", "chain_getFinalizedHead", "chain_getBlock", "state_getRuntimeVersion", "state_getMetadata", "state_getStorage", "state_getStorageHash", "state_call":
	default:
		return fmt.Errorf("native capture refuses non-read method %q", method)
	}
	parameters, err := json.Marshal(args)
	if err != nil || uint64(len(parameters)) > self.maximum {
		return errors.Join(errors.New("native capture request exceeds its bound"), err)
	}
	callCtx, cancel := context.WithCancel(ctx)
	cancelled := make(chan struct{})
	stop := context.AfterFunc(self.ctx, func() { defer close(cancelled); cancel() })
	defer func() {
		if !stop() {
			<-cancelled
		}
		cancel()
	}()
	// The upstream transport unmarshals through its result interface. A
	// nullable destination makes an explicit Json null observable without
	// confusing it with a transport that never wrote a response at all.
	value := &releaseNativeCaptureResultV2{maximum: self.maximum}
	if err := self.Client.CallContext(callCtx, &value, method, args...); err != nil {
		return errors.Join(err, callCtx.Err(), self.ctx.Err())
	}
	if value == nil {
		value = &releaseNativeCaptureResultV2{maximum: self.maximum}
		if err := value.UnmarshalJSON([]byte("null")); err != nil {
			return err
		}
	}
	if len(value.encoded) == 0 {
		return errors.New("native capture transport omitted its result")
	}
	if err := self.retain(callCtx, ReleaseEvidenceV2NativeRead{Schema: "urnetwork-validator-native-read-v2", Method: method, Parameters: parameters, Result: value.encoded}); err != nil {
		return err
	}
	if err := errors.Join(callCtx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	return json.Unmarshal(value.encoded, result)
}

// Builds only read facades. Copying the original RPC pointer would silently
// let convenience methods bypass capture; constructing NewRPC probes latest.
func releaseNativeCaptureChainV2(ctx context.Context, native *crv4.Chain, maximum uint64, retain func(context.Context, ReleaseEvidenceV2NativeRead) error) (*crv4.Chain, error) {
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || retain == nil || maximum == 0 {
		return nil, errors.New("native source capture owner is absent")
	}
	client := &releaseNativeCaptureClientV2{Client: native.API.Client, ctx: ctx, maximum: maximum, retain: retain}
	owned := *native
	owned.API = &gsrpc.SubstrateAPI{Client: client, RPC: &gsrpcrpc.RPC{Chain: gsrpcchain.NewChain(client), State: gsrpcstate.NewState(client)}}
	return &owned, nil
}

// Retains actual prepared-source/finalized storage, block, events, schedule,
// permit and calculated-stake reads through the established verifier. This
// proves native source custody, not attempt scoring or final semantic success.
func CaptureReleaseNativeSourceV2(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, intent *SteeringIntent, measurement []byte, retain func(context.Context, ReleaseEvidenceV2NativeRead) error) error {
	if cfg == nil || intent == nil || intent.Prepared == nil || ctx == nil {
		return errors.New("native source capture intent is incomplete")
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, cfg.EvidenceV2.Bounds.MaxArtifactBytes, cfg.EvidenceV2.Bounds.MaxOperators)
	if err != nil {
		return err
	}
	owned, err := releaseNativeCaptureChainV2(ctx, native, cfg.EvidenceV2.Bounds.MaxControlBytes, retain)
	if err != nil {
		return err
	}
	// Runtime caching may avoid another metadata response. Capture its exact
	// pinned wire explicitly, then the normal verifier checks the reviewed hash.
	seen := map[string]bool{}
	for _, hash := range []string{artifact.NativeSnapshotHash, intent.Prepared.PreparedAtBlockHash, intent.FinalizedBlockHash} {
		if hash == "" || seen[hash] {
			continue
		}
		if _, err := canonicalAttemptHex32("native capture block hash", hash, false); err != nil {
			return err
		}
		seen[hash] = true
		var raw string
		if err := owned.API.Client.CallContext(ctx, &raw, "state_getMetadata", hash); err != nil {
			return err
		}
		_, metadataHash, err := crv4.DecodeRuntimeMetadata(raw)
		if err != nil || !strings.EqualFold(metadataHash, cfg.RuntimeMetadataHash) {
			return errors.Join(errors.New("captured native metadata differs from the reviewed original bytes"), err)
		}
	}
	hash, err := types.NewHashFromHexString(artifact.NativeSnapshotHash)
	if err != nil {
		return err
	}
	hotkey, err := canonicalAttemptHex32("native source capture hotkey", intent.Prepared.HotkeyHex, false)
	if err != nil {
		return err
	}
	schedule, err := crv4.ReadValidatorScheduleAtContext(ctx, owned, crv4.ValidatorScheduleQuery{GenesisHash: owned.GenesisHash, BlockHash: hash, BlockNumber: artifact.NativeSnapshotBlock, Netuid: cfg.Netuid, Hotkey: hotkey, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs}, releaseRuntimeIdentityV2(cfg))
	if err != nil || !schedule.Stake.MeetsNonSelfStakeAndPermit() || schedule.SubnetEpochIndex != artifact.SubnetEpoch || schedule.Stake.Identity.UID != artifact.SelfUID {
		return errors.Join(errors.New("compact captured decision lacks actual native schedule/eligibility"), err)
	}
	return authenticateReleaseNativeSourceReferenceV2(ctx, owned, cfg, intent, artifact)
}
