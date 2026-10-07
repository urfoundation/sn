// Readiness borrows existing custody under shared local locks. It cannot finish
// interrupted claims, import signatures, write progress or renew allowances.
package main

import (
	"context"
	"errors"
)

// These seals identify the actual validated children, including original signed
// bytes when present. They are local evidence, never current chain authority.
type bootstrapChainReadinessState struct {
	PreparationHash         string `json:"preparation_hash"`
	ContractsHash           string `json:"contracts_hash"`
	RootProgressHash        string `json:"root_progress_hash"`
	RootCustodyHash         string `json:"root_custody_hash"`
	RootServiceHash         string `json:"root_service_hash"`
	ContractTransactionHash string `json:"contract_transaction_hash,omitempty"`
	RootExtrinsicHash       string `json:"root_extrinsic_hash,omitempty"`
	RootStrategy            string `json:"root_strategy,omitempty"`
	locks                   []*bootstrapContractReadinessMarker
	journals                []bootstrapReadinessJournal
	failed                  error
	closed                  bool
}

// Original journals are immutable while their markers are borrowed. Their
// exact bytes include signed intent, counters and any original completion.
type bootstrapReadinessJournal struct {
	path   string
	digest string
	limit  int
}

// A canceled read remains retryable. Observed custody loss poisons this owner,
// even if another actor restores the original inode or bytes afterward.
func (self *bootstrapChainReadinessState) checkpoint(ctx context.Context) error {
	if self == nil || self.closed || len(self.locks) == 0 || len(self.locks) != len(self.journals) {
		return errors.New("bootstrap readiness original custody is absent or closed")
	}
	if self.failed != nil {
		return self.failed
	}
	if ctx == nil {
		return errors.New("bootstrap readiness context is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fail := func(err error) error {
		if mainnetDurableAdmissionPending(err) {
			return err
		}
		self.failed = errors.Join(errRpcIntegrity, errors.New("bootstrap readiness original custody changed; retain original intent and reconcile custody"), err)
		return self.failed
	}
	for i, journal := range self.journals {
		if err := self.locks[i].checkpoint(); err != nil {
			return fail(err)
		}
		_, digest, err := self.locks[i].storage.readFile(ctx, journal.path, journal.limit)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if err != nil || digest != journal.digest {
			return fail(errors.Join(errors.New("bootstrap readiness completed original journal changed or disappeared"), err))
		}
	}
	// The last journal read must not hide a changed earlier marker.
	for _, lock := range self.locks {
		if err := lock.checkpoint(); err != nil {
			return fail(err)
		}
	}
	return ctx.Err()
}

// Legacy observations require real custody. Passive observations identify the
// different strategy explicitly and cannot claim any native root liability.
func (self bootstrapChainReadinessState) validRootSeals() bool {
	if !planSha256(self.RootProgressHash) || !planSha256(self.RootServiceHash) {
		return false
	}
	if self.RootStrategy == rootPassiveStrategy {
		return self.RootCustodyHash == "" && self.RootExtrinsicHash == ""
	}
	return self.RootStrategy == "" && planSha256(self.RootCustodyHash)
}

// Release only locks opened by this read. No retained marker is removed.
func (self *bootstrapChainReadinessState) close() error {
	if self == nil || self.closed {
		return nil
	}
	self.closed = true
	var result error
	for i := len(self.locks) - 1; i >= 0; i-- {
		result = errors.Join(result, self.locks[i].close())
	}
	self.locks = nil
	return result
}

// All five original markers must be complete before any state is decoded.
// Shared ownership lasts through the complete bounded observation operation.
func openBootstrapChainReadinessState(ctx context.Context, preparation bootstrapChainPreparation) (_ *bootstrapChainReadinessState, resultErr error) {
	if ctx == nil {
		return nil, errors.New("bootstrap readiness context is absent")
	}
	if err := errors.Join(ctx.Err(), preparation.validate(), bootstrapRootDirectory(preparation.Plan.Config.RunDirectory)); err != nil {
		return nil, err
	}
	if !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) {
		return nil, errors.New("bootstrap readiness requires the original accepted v3/v4 preparation; older custody cannot acquire new role scope")
	}
	self := &bootstrapChainReadinessState{}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	service := preparation.Root.Service
	markers := []string{
		preparation.Plan.ContentHash + "\n" + bootstrapRootClaimComplete,
		rootObjectHash(preparation.Contracts.Config) + "\n" + bootstrapRootClaimComplete,
		preparation.Root.ContentHash + "\n" + bootstrapRootClaimComplete,
		rootObjectHash(service.CustodyTrust) + "\n" + service.Packet.ContentHash + "\n",
		rootObjectHash(service) + "\n",
	}
	if preparation.Root.PassiveService != nil {
		markers = markers[:3]
	}
	paths := preparation.childPaths()
	kinds := []string{"mainnet-bootstrap-chain", "mainnet-evm-action", "mainnet-bootstrap-root", "mainnet-root-offline", "mainnet-root-service"}
	limits := []int{rootServiceStoreLimit, 512 * 1024, 16 * 1024, rootOfflineStoreLimit, rootServiceStoreLimit}
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := openBootstrapReadinessSnapshot(path, markers[i], kinds[i], limits[i], ctx)
		if err != nil {
			return nil, errors.Join(errors.New("bootstrap readiness requires complete original markers without an active custody owner"), err)
		}
		self.locks = append(self.locks, lock)
	}
	var chain bootstrapChainRecord
	var contracts evmActionRecord
	var root bootstrapRootRecord
	var custody rootOfflineCustodyRecord
	var retainedService rootServiceRecord
	destinations := []any{&chain, &contracts, &root, &custody, &retainedService}
	if preparation.Root.PassiveService != nil {
		destinations = destinations[:3]
	}
	for i, destination := range destinations {
		raw, digest, err := self.locks[i].storage.readFile(ctx, paths[i], limits[i])
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if err != nil {
			if mainnetDurableAdmissionPending(err) {
				return nil, err
			}
			return nil, errors.Join(errRpcIntegrity, errors.New("bootstrap readiness completed original journal is unavailable"), err)
		}
		if err := decodePlanJson(raw, destination); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		self.journals = append(self.journals, bootstrapReadinessJournal{path: paths[i], digest: digest, limit: limits[i]})
	}
	if preparation.Root.PassiveService != nil {
		if err := errors.Join(chain.validate(preparation.Plan), contracts.validate(preparation.Contracts.Config), root.validate(preparation.Root)); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		if chain.Phase != "prepared" || root.Phase != "passive-service-retained" {
			return nil, errors.New("bootstrap readiness preparation is incomplete; resume its original owner")
		}
		if chain.RootExtrinsicHash != "" ||
			chain.ContractTransactionHash != "" && chain.ContractTransactionHash != contracts.TransactionHash {
			return nil, errors.Join(errRpcIntegrity, errors.New("passive readiness requires complete preparation without native root liabilities"))
		}
		self.PreparationHash, self.ContractsHash, self.RootProgressHash = chain.ContentHash, contracts.ContentHash, root.ContentHash
		self.RootServiceHash, self.ContractTransactionHash = preparation.Root.serviceHash(), contracts.TransactionHash
		self.RootStrategy = rootPassiveStrategy
		return self, self.checkpoint(ctx)
	}
	if err := errors.Join(chain.validate(preparation.Plan), contracts.validate(preparation.Contracts.Config), root.validate(preparation.Root),
		custody.validate(service.CustodyTrust), retainedService.validate(service)); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if chain.Phase != "prepared" || root.Phase != "service-retained" && root.Phase != "signature-retained" {
		return nil, errors.New("bootstrap readiness preparation is incomplete; resume its original owner")
	}
	if custody.Packet.ContentHash != service.Packet.ContentHash ||
		chain.ContractTransactionHash != "" && chain.ContractTransactionHash != contracts.TransactionHash ||
		chain.RootExtrinsicHash != "" && chain.RootExtrinsicHash != custody.ExtrinsicHash ||
		root.SignatureHash != "" && root.SignatureHash != custody.ExtrinsicHash ||
		retainedService.Action.Signature != "" && custody.Signature != "" && retainedService.Action.Signature != custody.Signature {
		return nil, errors.Join(errRpcIntegrity, errors.New("bootstrap readiness requires complete original custody and consistent retained signature lineage"))
	}
	self.PreparationHash, self.ContractsHash, self.RootProgressHash = chain.ContentHash, contracts.ContentHash, root.ContentHash
	self.RootCustodyHash, self.RootServiceHash = custody.ContentHash, retainedService.ContentHash
	self.ContractTransactionHash, self.RootExtrinsicHash = contracts.TransactionHash, custody.ExtrinsicHash
	return self, self.checkpoint(ctx)
}
