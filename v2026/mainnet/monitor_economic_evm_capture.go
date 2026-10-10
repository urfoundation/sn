// Capture identity is an explicitly requested, block-pinned getter companion.
// It shares the original code hash and owned-RPC authority of the receipt
// reader; it grants no independent EVM finality or native runtime authority.
package main

import (
	"context"
	"strings"
)

type monitorEconomicVaultCaptureIdentity struct {
	Address      string `json:"original_vault"`
	CodeHash     string `json:"original_code_hash"`
	Netuid       uint16 `json:"original_netuid"`
	PoolHotkey   string `json:"original_pool_hotkey"`
	Coldkey      string `json:"original_self_coldkey"`
	EscrowHotkey string `json:"original_escrow_hotkey"`
}

func (self *monitorEconomicVaultCaptureIdentity) validate(policy monitorEconomicEvmPolicy, event monitorEconomicEvmEvent) error {
	if self == nil {
		if policy.CaptureIdentity && event.Name == "EmissionCaptured" {
			return monitorEvmIntegrity("economic capture omitted original vault identity getters")
		}
		return nil
	}
	zero := "0x" + strings.Repeat("0", 64)
	if !policy.CaptureIdentity || event.Name != "EmissionCaptured" || self.Address != policy.Address || self.CodeHash != policy.CodeHash || self.Netuid != policy.Netuid || !rootCanonicalHash(self.Coldkey) || !rootCanonicalHash(self.EscrowHotkey) || self.Coldkey == zero || self.EscrowHotkey == zero || self.PoolHotkey != event.Values["poolHotkey"] || self.PoolHotkey == self.EscrowHotkey {
		return monitorEvmIntegrity("economic capture changed its original vault identity")
	}
	return nil
}

func (self *monitorEvmReader) captureIdentity(ctx context.Context, event monitorEconomicEvmEvent) (*monitorEconomicVaultCaptureIdentity, error) {
	if !self.policy.CaptureIdentity || event.Name != "EmissionCaptured" {
		return nil, nil
	}
	pool, err := monitorEconomicInteger(event.Values["noId"])
	if err != nil {
		return nil, err
	}
	values, err := self.getter(ctx, event.Block.Hash, "pools", pool)
	if err != nil {
		return nil, err
	}
	if len(values) != 3 {
		return nil, monitorEvmIntegrity("economic capture original pool getter differs")
	}
	hotkey, err := monitorEvmScalar(values[0])
	if err != nil {
		return nil, err
	}
	result := &monitorEconomicVaultCaptureIdentity{Address: self.policy.Address, CodeHash: self.policy.CodeHash, Netuid: self.policy.Netuid, PoolHotkey: hotkey}
	for _, field := range []struct {
		method string
		target *string
	}{{method: "selfColdkey", target: &result.Coldkey}, {method: "escrowHotkey", target: &result.EscrowHotkey}} {
		values, err := self.getter(ctx, event.Block.Hash, field.method)
		if err != nil {
			return nil, err
		}
		if len(values) != 1 {
			return nil, monitorEvmIntegrity("economic capture immutable identity getter differs")
		}
		*field.target, err = monitorEvmScalar(values[0])
		if err != nil {
			return nil, err
		}
	}
	return result, result.validate(self.policy, event)
}
