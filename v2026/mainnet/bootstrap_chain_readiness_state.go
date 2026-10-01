// Readiness borrows existing custody under shared local locks. It cannot finish
// interrupted claims, import signatures, write progress or renew allowances.
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
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
	locks                   []*os.File
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
	var result error
	for i := len(self.locks) - 1; i >= 0; i-- {
		result = errors.Join(result, self.locks[i].Close())
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
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fd, err := syscall.Open(path+".lock", syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, err
		}
		lock := os.NewFile(uintptr(fd), path+".lock")
		self.locks = append(self.locks, lock)
		info, err := lock.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.Join(errors.New("bootstrap readiness requires private regular retained markers"), err)
		}
		if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
			return nil, errors.Join(errors.New("bootstrap readiness conflicts with an active custody owner"), err)
		}
		raw, err := io.ReadAll(io.LimitReader(lock, int64(len(markers[i])+1)))
		if err != nil || string(raw) != markers[i] {
			return nil, errors.Join(errors.New("bootstrap readiness marker is incomplete or belongs to another accepted preparation"), err)
		}
	}
	var chain bootstrapChainRecord
	var contracts evmActionRecord
	var root bootstrapRootRecord
	var custody rootOfflineCustodyRecord
	var retainedService rootServiceRecord
	limits := []int{rootServiceStoreLimit, 512 * 1024, 16 * 1024, rootOfflineStoreLimit, rootServiceStoreLimit}
	destinations := []any{&chain, &contracts, &root, &custody, &retainedService}
	if preparation.Root.PassiveService != nil {
		destinations = destinations[:3]
	}
	for i, destination := range destinations {
		raw, _, err := readBootstrapRootFile(ctx, paths[i], limits[i])
		if err != nil {
			return nil, err
		}
		if err := decodePlanJson(raw, destination); err != nil {
			return nil, err
		}
	}
	if preparation.Root.PassiveService != nil {
		if err := errors.Join(chain.validate(preparation.Plan), contracts.validate(preparation.Contracts.Config), root.validate(preparation.Root)); err != nil {
			return nil, err
		}
		if chain.Phase != "prepared" || root.Phase != "passive-service-retained" || chain.RootExtrinsicHash != "" ||
			chain.ContractTransactionHash != "" && chain.ContractTransactionHash != contracts.TransactionHash {
			return nil, errors.New("passive readiness requires complete preparation without native root liabilities")
		}
		self.PreparationHash, self.ContractsHash, self.RootProgressHash = chain.ContentHash, contracts.ContentHash, root.ContentHash
		self.RootServiceHash, self.ContractTransactionHash = preparation.Root.serviceHash(), contracts.TransactionHash
		self.RootStrategy = rootPassiveStrategy
		return self, ctx.Err()
	}
	if err := errors.Join(chain.validate(preparation.Plan), contracts.validate(preparation.Contracts.Config), root.validate(preparation.Root),
		custody.validate(service.CustodyTrust), retainedService.validate(service)); err != nil {
		return nil, err
	}
	if chain.Phase != "prepared" || root.Phase != "service-retained" && root.Phase != "signature-retained" || custody.Packet.ContentHash != service.Packet.ContentHash ||
		chain.ContractTransactionHash != "" && chain.ContractTransactionHash != contracts.TransactionHash ||
		chain.RootExtrinsicHash != "" && chain.RootExtrinsicHash != custody.ExtrinsicHash ||
		root.SignatureHash != "" && root.SignatureHash != custody.ExtrinsicHash ||
		retainedService.Action.Signature != "" && custody.Signature != "" && retainedService.Action.Signature != custody.Signature {
		return nil, errors.New("bootstrap readiness requires complete original custody and consistent retained signature lineage")
	}
	self.PreparationHash, self.ContractsHash, self.RootProgressHash = chain.ContentHash, contracts.ContentHash, root.ContentHash
	self.RootCustodyHash, self.RootServiceHash = custody.ContentHash, retainedService.ContentHash
	self.ContractTransactionHash, self.RootExtrinsicHash = contracts.TransactionHash, custody.ExtrinsicHash
	return self, ctx.Err()
}
