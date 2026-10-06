// Upload admission projects independently pinned production authority into
// immutable read-only runtime windows. No signer or producer capsule survives.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Runtime identity belongs to its approved native interval, even when a later
// interval reuses the same bytes. Earlier windows cannot authorize current reads.
type validatorUploadRuntimeWindow struct {
	from     uint64
	through  uint64
	artifact crv4.RuntimeArtifactIdentity
}

// The loader owns these values. Retaining a complete ReleaseConfig would also
// retain its signing capsule, so only the finite observation inputs are copied.
type validatorUploadRuntimeAuthority struct {
	deployment   ValidatorUploadDeployment
	nativeChain  string
	nativeRoutes []string
	current      validatorUploadRuntimeWindow
	history      []validatorUploadRuntimeWindow
}

// Parse exactly the content-addressed bytes, with the existing signature and
// history loaders. Config paths name public approvals; seed files are not read.
func loadValidatorUploadRuntimeContext(ctx context.Context, deployment ValidatorUploadDeployment, reference ReleaseEvidenceV2File) (ValidatorUploadDeployment, error) {
	if ctx == nil {
		return ValidatorUploadDeployment{}, errors.New("validator staging runtime context is absent")
	}
	if err := errors.Join(ctx.Err(), deployment.Validate()); err != nil {
		return ValidatorUploadDeployment{}, err
	}
	if deployment.ChainID != 964 {
		if deployment.productionRuntime != nil || reference != (ReleaseEvidenceV2File{}) {
			return ValidatorUploadDeployment{}, errors.New("production staging history cannot authorize another chain")
		}
		return deployment, nil
	}
	raw, err := ReadReleaseEvidenceV2File(ctx, reference, maximumReleaseConfigBytes)
	if err != nil {
		return ValidatorUploadDeployment{}, err
	}
	cfg, err := decodeReleaseConfigBytes(reference.Path, raw)
	if err != nil {
		return ValidatorUploadDeployment{}, err
	}
	if err := validateReleaseProductionRuntimeHistory(cfg); err != nil {
		return ValidatorUploadDeployment{}, err
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return ValidatorUploadDeployment{}, err
	}
	genesis, err := parseHash32("validator staging production genesis", cfg.GenesisHash)
	if err != nil {
		return ValidatorUploadDeployment{}, err
	}
	if cfg.ChainID != deployment.ChainID || genesis != deployment.GenesisHash || cfg.Netuid != deployment.Netuid ||
		[20]byte(common.HexToAddress(cfg.Coordinator)) != deployment.Coordinator || [20]byte(common.HexToAddress(cfg.SettlementVault)) != deployment.SettlementVault ||
		sha256.Sum256([]byte(cfg.DeploymentID)) != deployment.DeploymentIDHash || releaseNativeRuntimeIdentity(cfg) != deployment.NativeRuntime ||
		deployment.MaximumSubnetUIDs > approved.Approval.MaximumSubnetUids {
		return ValidatorUploadDeployment{}, errors.New("validator staging runtime config differs from the independently approved deployment")
	}
	deployment.productionRuntime = nil
	owner := &validatorUploadRuntimeAuthority{deployment: deployment, nativeChain: approved.Approval.NativeChain, nativeRoutes: slices.Clone(cfg.Substrate),
		current: validatorUploadRuntimeWindow{from: approved.Approval.ValidFromNativeBlock, through: approved.Approval.ValidThroughNativeBlock, artifact: deployment.NativeRuntime}}
	windows, err := productionHistoricalRuntimeWindows(cfg)
	if err != nil {
		return ValidatorUploadDeployment{}, err
	}
	for _, window := range windows {
		owner.history = append(owner.history, validatorUploadRuntimeWindow{from: window.from, through: window.through, artifact: window.artifact})
	}
	if err := ctx.Err(); err != nil {
		return ValidatorUploadDeployment{}, err
	}
	deployment.productionRuntime = owner
	return deployment, nil
}

// A copied deployment cannot substitute a public tuple or scope around a
// privately loaded projection. A mainnet tuple alone grants no history.
func (self ValidatorUploadDeployment) runtimeAuthority() (*validatorUploadRuntimeAuthority, error) {
	owner := self.productionRuntime
	if self.ChainID != 964 || owner == nil {
		return nil, errors.New("mainnet staging requires independently loaded read-only runtime history")
	}
	self.productionRuntime = nil
	if self != owner.deployment {
		return nil, errors.New("validator staging deployment changed after runtime authentication")
	}
	return owner, nil
}

// Ordinary testnet readers keep their existing exact compiled history. Mainnet
// selects one signed interval, without a catalog fallback or version inference.
func (self ValidatorUploadDeployment) runtimeArtifactsAt(number uint64, historical bool) ([]crv4.RuntimeArtifactIdentity, error) {
	if self.ChainID != 964 {
		if self.productionRuntime != nil {
			return nil, errors.New("production staging history cannot authorize another chain")
		}
		if historical {
			return HistoricalReleaseRuntimeArtifacts(self.NativeRuntime), nil
		}
		return []crv4.RuntimeArtifactIdentity{self.NativeRuntime}, nil
	}
	owner, err := self.runtimeAuthority()
	if err != nil {
		return nil, err
	}
	if owner.current.from <= number && number <= owner.current.through {
		return []crv4.RuntimeArtifactIdentity{owner.current.artifact}, nil
	}
	if historical {
		for _, window := range owner.history {
			if window.from <= number && number <= window.through {
				return []crv4.RuntimeArtifactIdentity{window.artifact}, nil
			}
		}
	}
	return nil, errors.New("validator staging native block is outside its approved runtime interval")
}

// Fresh route identity precedes exact-block reads. This is an observation
// capability; it never binds a producer runtime or consults a private key.
func (self ValidatorUploadDeployment) authenticateNativeRuntimeRouteContext(ctx context.Context, native *crv4.Chain) error {
	if ctx == nil {
		return errors.New("validator staging native route context is absent")
	}
	if self.ChainID != 964 {
		if self.productionRuntime != nil {
			return errors.New("production staging history cannot authorize another chain")
		}
		return ctx.Err()
	}
	owner, err := self.runtimeAuthority()
	if err != nil {
		return err
	}
	if native == nil || native.API == nil || native.API.Client == nil || native.ProvisionalRuntimeCompatibilityEnabled() ||
		native.GenesisHash != types.Hash(self.GenesisHash) || !slices.Contains(owner.nativeRoutes, native.API.Client.URL()) {
		return errors.New("validator staging native route differs from signed production authority")
	}
	_, err = readRuntimeNetworkIdentity(ctx, native, owner.nativeChain, types.Hash(self.GenesisHash))
	return err
}
