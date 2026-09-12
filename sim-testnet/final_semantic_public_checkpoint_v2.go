//go:build linux || darwin

package main

import (
	"context"
	"errors"
	"net/http"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/crv4"
	validatorpkg "github.com/urfoundation/sn/validator"
)

var _ finalNativeCheckpointReaderV2 = (*PublicFinalSemanticChainReader)(nil)

// NativeCheckpointV2 runs the shared exact runtime/clock/row reader through
// independently admitted public clients. Every immutable RPC response joins
// the normal transcript; errors discard the entire partial observation.
func (self *PublicFinalSemanticChainReader) NativeCheckpointV2(ctx context.Context, evidence *FinalSemanticEvidence, at ChainHead, validatorUID uint16, hotkey string) (observation FinalNativeCheckpointV2, exchanges []FinalRPCExchange, resultErr error) {
	if ctx == nil || self == nil || self.evm == nil || self.native == nil || self.native.Client == nil {
		return observation, nil, errors.New("public native checkpoint reader is unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil { observation, exchanges = FinalNativeCheckpointV2{}, nil }
	}()
	owned, err := self.finalV2InvocationReader(evidence)
	if err != nil { return observation, nil, err }
	key, err := decodeHex32("public native checkpoint hotkey", hotkey)
	if err != nil || key == ([32]byte{}) { return observation, nil, errors.Join(errors.New("public native checkpoint hotkey is invalid"), err) }
	genesis, err := gsrpctypes.NewHashFromHexString(owned.evidence.GenesisHash)
	if err != nil { return observation, nil, err }
	// This capability has the existing public artifact/transcript byte bound;
	// the shared typed readers additionally enforce runtime UID/SCALE bounds.
	recorder := newFinalV2RPCRecorder(ctx, owned, uint64(maximumCampaignEvidenceRawFileBytes), uint64(maximumCampaignEvidenceRawFileBytes))
	transport, err := rpc.DialOptions(ctx, "http://validator-evidence-read.invalid", rpc.WithHTTPClient(&http.Client{Transport: &finalV2EVMReadTransport{owner: recorder}}))
	if err != nil { return observation, nil, err }
	defer transport.Close()
	evmReader := *owned
	evmReader.evm = transport
	if _, err := evmReader.CanonicalEVMHead(ctx, at); err != nil { return observation, nil, err }
	native, err := validatorpkg.NewReleaseNativeReadRecorderV2(ctx, &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: owned.native.Client}, GenesisHash: genesis}, recorder.maximum, recorder.retainNative)
	if err != nil { return observation, nil, err }
	// These exact pins were authenticated from the public deployment manifest
	// by the reader constructor. Dial-time or captured metadata is not authority.
	runtime := crv4.RuntimeArtifactIdentity{Version: owned.runtimeVersion, CodeHash: owned.runtimeCodeHash, MetadataHash: owned.runtimeMetadataHash}
	observation, err = readFinalNativeCheckpointV2(ctx, native, at, owned.evidence.Netuid, validatorUID, key, runtime)
	if err != nil { return observation, nil, err }
	exchanges, err = recorder.finish()
	return observation, exchanges, err
}
