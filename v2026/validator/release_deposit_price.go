// The signed price sheet sets the demand deposit price, and the signed policy
// document never changes: a new hash would orphan every miner consent
// (VERSION2-FINAL.md §2.2). So the policy in force for a decision epoch is the
// document with the sheet's signed entry for that epoch laid over its deposit
// schedule. Rates come from the decision epoch's entry and price the previous
// epoch's usage. That copy judges deposits and is never hashed.
//
// A measurement artifact carries the entry it used, so anyone can check its rates
// offline under the deployment's pinned signer. An artifact with no entry was
// judged by the document alone and keeps the bytes it always had.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urfoundation/sn/v2026/demand/pricesheet"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The error each steering pass logs while a signed price is in force and the
// configuration names no settlement v2 contracts.
const releaseDepositPriceWithoutSettlementV2 = "a signed deposit price is in force, but this configuration has no settlement_v2: a source epoch that settlement v2 settles is audited as a v1 epoch, which has no payout artifact, so its pool gets no weight"

const (
	// A steering pass reads the published sheet at most this often, however often
	// it retries, and for no longer than the timeout. Neither can use up the
	// steering budget (production_steering_read.go).
	releaseDepositPriceCacheTTL       = 5 * time.Minute
	releaseDepositPriceRefreshTimeout = 10 * time.Second
)

// Each deployment's pinned signer and the sheet's published address. Tests point
// them at their own key and server once, from an init, before any test runs.
var (
	releaseDepositPriceSigner = pricesheet.SignerFor
	releaseDepositPriceURL    = pricesheet.URL
)

// ReleaseDepositPriceEntry is the signed price in force for a decision, as its
// measurement artifact carries it.
type ReleaseDepositPriceEntry struct {
	EffectiveEpoch uint64                 `json:"effective_epoch"`
	EpochCapRao    uint64                 `json:"epoch_cap_rao"`
	Tiers          []protocol.DepositTier `json:"tiers"`
	// Signature is the owner's EIP-191 signature of the entry's plain text line.
	Signature string `json:"signature"`
}

// newReleaseDepositPriceEntry is entry's wire form, nil for no entry.
func newReleaseDepositPriceEntry(entry *pricesheet.Entry) *ReleaseDepositPriceEntry {
	if entry == nil {
		return nil
	}
	return &ReleaseDepositPriceEntry{EffectiveEpoch: entry.EffectiveEpoch, EpochCapRao: entry.EpochCapRao, Tiers: slices.Clone(entry.Tiers), Signature: "0x" + hex.EncodeToString(entry.Signature)}
}

// verifiedReleaseDepositPriceEntry authenticates a carried entry: it must be signed
// by the deployment's pinned signer and already in effect at epoch. No entry is
// nil and valid; an entry on a deployment with no pinned signer is refused.
func verifiedReleaseDepositPriceEntry(wire *ReleaseDepositPriceEntry, chainID uint64, netuid uint16, epoch uint64) (*pricesheet.Entry, error) {
	if wire == nil {
		return nil, nil
	}
	signer, pinned := releaseDepositPriceSigner(chainID, netuid)
	if !pinned {
		return nil, errors.New("signed deposit price names a deployment with no pinned signer")
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(wire.Signature, "0x"))
	if err != nil || "0x"+hex.EncodeToString(signature) != wire.Signature {
		return nil, errors.New("signed deposit price signature is not canonical hex")
	}
	entry := &pricesheet.Entry{EffectiveEpoch: wire.EffectiveEpoch, EpochCapRao: wire.EpochCapRao, Tiers: slices.Clone(wire.Tiers), Signature: signature}
	if err := entry.Verify(chainID, netuid, signer); err != nil {
		return nil, fmt.Errorf("signed deposit price: %w", err)
	}
	if entry.EffectiveEpoch > epoch {
		return nil, fmt.Errorf("signed deposit price takes effect at epoch %d, after epoch %d", entry.EffectiveEpoch, epoch)
	}
	return entry, nil
}

// releaseEffectiveDepositPolicy is the deposit policy in force under entry: its
// schedule and cap, or base when no signed entry applies. Its fixed audit rules,
// the usage lag among them, are always base's.
func releaseEffectiveDepositPolicy(entry *pricesheet.Entry, base protocol.DepositPolicy) protocol.DepositPolicy {
	if entry == nil {
		return base
	}
	return entry.DepositPolicy(base)
}

// releaseMeasurementDepositPolicy is the deposit policy artifact's audits were
// judged under, its carried entry authenticated first.
func releaseMeasurementDepositPolicy(artifact *ReleaseMeasurementArtifact) (protocol.DepositPolicy, error) {
	entry, err := verifiedReleaseDepositPriceEntry(artifact.DepositPriceEntry, artifact.ChainID, artifact.Netuid, artifact.SettlementEpoch)
	if err != nil {
		return protocol.DepositPolicy{}, err
	}
	return releaseEffectiveDepositPolicy(entry, artifact.Policy.Deposit), nil
}

// releaseScoringPolicy is policy with deposit in force, for pricing a pool's
// weight. It is not a signed document: nothing may hash it.
func releaseScoringPolicy(policy protocol.Policy, deposit protocol.DepositPolicy) protocol.Policy {
	policy.Deposit = deposit
	return policy
}

// releaseDepositCapsSuffice judges the coordinator's deposit caps against the cap in
// force: the signed entry's, or the document's when no entry is in force. The epoch
// cap must hold at least that cap, or the owed deposit could not be paid, and the
// campaign cap at least the document's and one epoch cap. Caps equal to the
// document's pass as they always have, and a raise may take effect before the entry
// that needs it.
func releaseDepositCapsSuffice(epochCap, campaignCap *big.Int, base protocol.DepositPolicy, entry *pricesheet.Entry) bool {
	inForce := base.EpochCapRaoPerOperator
	if entry != nil {
		inForce = entry.EpochCapRao
	}
	campaignFloor := new(big.Int).SetUint64(base.TotalTestCampaignCapRao)
	if epochCap.Cmp(campaignFloor) > 0 {
		campaignFloor = epochCap
	}
	return epochCap.Cmp(new(big.Int).SetUint64(inForce)) >= 0 && campaignCap.Cmp(campaignFloor) >= 0
}

// releaseDepositPriceStore keeps the last verified sheet in the validator's state
// directory. A sheet read back is verified again before use.
type releaseDepositPriceStore struct {
	path string
}

func (self *releaseDepositPriceStore) Load(ctx context.Context) ([]byte, error) {
	raw, err := os.ReadFile(self.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return raw, ctx.Err()
}

func (self *releaseDepositPriceStore) Save(ctx context.Context, raw []byte) error {
	if existing, err := os.ReadFile(self.path); err == nil && bytes.Equal(existing, raw) {
		return ctx.Err()
	}
	return atomicStateWrite(self.path, raw, 0o600)
}

// releaseDepositPrices holds one deployment's signed price sheet for steering. A
// nil value is a deployment with no pinned signer: it never fetches anything, and
// its signed policy's schedule stays in force.
type releaseDepositPrices struct {
	source  *pricesheet.Source
	now     func() time.Time
	timeout time.Duration

	// One refresh at a time. It is never held while the sheet is read, so a slow
	// fetch does not hold up entryAt.
	refreshing sync.Mutex
	lock       sync.Mutex
	sheet      *pricesheet.Sheet
	refreshed  time.Time
}

// newReleaseDepositPrices returns nil unless cfg's deployment has a pinned signer.
func newReleaseDepositPrices(cfg *ReleaseConfig) *releaseDepositPrices {
	signer, pinned := releaseDepositPriceSigner(cfg.ChainID, cfg.Netuid)
	if !pinned {
		return nil
	}
	store := &releaseDepositPriceStore{path: filepath.Join(cfg.StateDir, "price-sheet", "price.yml")}
	return &releaseDepositPrices{source: pricesheet.NewSource(releaseDepositPriceURL, cfg.ChainID, cfg.Netuid, signer, store), now: time.Now, timeout: releaseDepositPriceRefreshTimeout}
}

// refresh reads the published sheet, at most once per cache interval. A failure is
// logged and never fails steering: the last verified sheet stays in force, and if
// nothing was ever verified the signed policy's schedule does.
func (self *releaseDepositPrices) refresh(ctx context.Context) {
	if self == nil {
		return
	}
	self.refreshing.Lock()
	defer self.refreshing.Unlock()
	now := self.now()
	self.lock.Lock()
	due := self.refreshed.IsZero() || now.Sub(self.refreshed) >= releaseDepositPriceCacheTTL
	held := self.sheet
	self.lock.Unlock()
	if !due {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, self.timeout)
	defer cancel()
	sheet, problem := self.source.Refresh(bounded)
	// A sheet that drops or changes an entry this process already verified, or adds
	// one before it, is refused even when a lost store could not show it. A caller's
	// own cancellation says nothing of the site, so it does not start the interval.
	if sheet != nil && !pricesheet.Extends(sheet, held) {
		refused := errors.New("pricesheet: the published sheet drops or changes a signed entry, or adds one before it")
		if problem != nil {
			refused = fmt.Errorf("%v; %w", problem, refused)
		}
		sheet, problem = nil, refused
	}
	self.lock.Lock()
	if sheet != nil {
		self.sheet = sheet
	}
	if ctx.Err() == nil {
		self.refreshed = now
	}
	kept := self.sheet != nil
	self.lock.Unlock()
	if problem != nil {
		fallback := "the signed policy's schedule stays in force"
		if kept {
			fallback = "the last verified sheet stays in force"
		}
		fmt.Printf("release steer: deposit price sheet: %v; %s\n", problem, fallback)
	}
}

// entryAtEpoch is entryAt for the coordinator's epoch. It needs the epoch only when
// the kept sheet has an entry, so it can never stop a pass that has no price to
// look up.
func (self *releaseDepositPrices) entryAtEpoch(epoch *big.Int) (*pricesheet.Entry, error) {
	if self == nil {
		return nil, nil
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	if self.sheet == nil || len(self.sheet.Entries) == 0 {
		return nil, nil
	}
	if epoch == nil || !epoch.IsUint64() {
		return nil, errors.New("settlement epoch is out of range")
	}
	return self.sheet.At(epoch.Uint64()), nil
}

// entryAt is the signed entry in force for epoch from the sheet the last refresh
// kept, nil for none. It reads nothing, so every use within a steering pass sees
// the price that pass's refresh found.
func (self *releaseDepositPrices) entryAt(epoch uint64) *pricesheet.Entry {
	if self == nil {
		return nil
	}
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.sheet.At(epoch)
}
