//go:build linux || darwin

// Receipt recovery keeps original signed source authority separate from the
// runtimes executing the inclusion block and installed in its final state.
package validator

import (
	"context"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// Complete headers keep an upgrade digest from stranding retained work. This
// generic bind grants historical reading only and never producer authority.
func authenticateProductionSourceRuntimeAtContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash) error {
	artifact, _, err := authenticateOwnerRecycleProductionArtifactWithHeadersAtContext(ctx, native, cfg, block, true, true)
	if err != nil {
		return err
	}
	return native.BindRuntimeArtifact(artifact)
}

// The caller already bound the original preparation under its complete signed
// authority. Both later views independently select their signed block windows;
// no current tuple or version ordering supplies historical authority.
func authenticateProductionFinalizedSourceContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, prepared *crv4.PreparedSubmission, receipt *crv4.FinalizedExtrinsic) error {
	if receipt == nil || native == nil || prepared == nil || ctx == nil {
		return errors.New("production source receipt context is incomplete")
	}
	number, parent, err := native.ReceiptHeaderAtContext(ctx, receipt.BlockHash)
	if err != nil {
		return err
	}
	if number != receipt.BlockNumber {
		return errors.New("production source receipt height differs from its authenticated header")
	}
	execution, executionNumber, err := authenticateOwnerRecycleProductionArtifactWithHeadersAtContext(ctx, native, cfg, parent, true, true)
	if err != nil {
		return err
	}
	if executionNumber+1 != number {
		return errors.New("production source receipt parent height differs from its execution boundary")
	}
	postState, _, err := authenticateOwnerRecycleProductionArtifactWithHeadersAtContext(ctx, native, cfg, receipt.BlockHash, true, true)
	if err != nil {
		return err
	}
	return native.VerifyFinalizedSourceRuntimeContext(ctx, prepared, receipt, execution, postState)
}
