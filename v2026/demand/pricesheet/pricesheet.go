// Package pricesheet reads the published demand deposit price at
// https://ur.xyz/price.yml. Operators size their deposits from it and validators
// audit them against it, so both must see the same rates for an epoch.
//
// The file itself is not trusted. Only entries of its signed_schedule count, each
// signed by the subnet owner's key over a line of plain text naming its prices, so
// a person signing on a hardware wallet reads exactly what they approve. Each entry
// takes effect from an epoch and stays in force until a later one does. A sheet
// with no entries, as at launch, leaves the signed policy's schedule in force.
package pricesheet

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/protocol"
)

const (
	// URL is where the sheet is published.
	URL = "https://ur.xyz/price.yml"

	maxSheetBytes = 256 << 10
	maxTiers      = 8
	raoDecimals   = 9
	maxUint64Text = 20

	mainnetChainID uint64 = 964
	mainnetNetuid  uint16 = 25
)

// MainnetSigner is the key that signs SN25's prices: the sole owner of the Safe
// that owns the coordinator, the authority that can already schedule its policy.
var MainnetSigner = common.HexToAddress("0x16C372dbBb24cd8473345ab13971E40814C8658F")

// SignerFor is the pinned signer of a deployment. A deployment with none has no
// published price, and its signed policy's schedule stays in force.
func SignerFor(chainID uint64, netuid uint16) (common.Address, bool) {
	if chainID == mainnetChainID && netuid == mainnetNetuid {
		return MainnetSigner, true
	}
	return common.Address{}, false
}

// Entry is one signed price.
type Entry struct {
	// EffectiveEpoch is the first epoch this price applies to.
	EffectiveEpoch uint64
	// EpochCapRao caps one operator's deposit for one epoch.
	EpochCapRao uint64
	// Tiers is the schedule, thresholds and rates in rao over one denominator.
	Tiers []protocol.DepositTier
	// Signature is an EIP-191 signature of Message.
	Signature []byte
}

// Sheet is a parsed price sheet: its signed entries in order of effective epoch.
type Sheet struct {
	Netuid  uint16
	Entries []Entry
	// Mirror is the unsigned schedule the file shows at its top level for people
	// and the site. It is never used to price anything.
	Mirror []protocol.DepositTier
}

// At is the entry in force for an epoch: the latest to take effect by then. Nil
// means no signed price applies yet.
func (s *Sheet) At(epoch uint64) *Entry {
	if s == nil {
		return nil
	}
	var found *Entry
	for i := range s.Entries {
		if s.Entries[i].EffectiveEpoch <= epoch {
			found = &s.Entries[i]
		}
	}
	return found
}

// Newest is the entry with the latest effective epoch, nil when there is none.
func (s *Sheet) Newest() *Entry {
	if s == nil || len(s.Entries) == 0 {
		return nil
	}
	return &s.Entries[len(s.Entries)-1]
}

// Verify checks that every entry is for this deployment and signed by signer.
func (s *Sheet) Verify(chainID uint64, netuid uint16, signer common.Address) error {
	if s == nil {
		return errors.New("pricesheet: no sheet")
	}
	if s.Netuid != netuid {
		return fmt.Errorf("pricesheet: the sheet is for sn %d, not sn %d", s.Netuid, netuid)
	}
	for i := range s.Entries {
		if err := s.Entries[i].Verify(chainID, netuid, signer); err != nil {
			return err
		}
	}
	return nil
}

// Verify checks the entry's signature.
func (e *Entry) Verify(chainID uint64, netuid uint16, signer common.Address) error {
	message, err := e.Message(chainID, netuid)
	if err != nil {
		return err
	}
	if len(e.Signature) != crypto.SignatureLength {
		return fmt.Errorf("pricesheet: entry for epoch %d has a %d-byte signature", e.EffectiveEpoch, len(e.Signature))
	}
	signature := slices.Clone(e.Signature)
	if signature[crypto.RecoveryIDOffset] >= 27 {
		signature[crypto.RecoveryIDOffset] -= 27
	}
	public, err := crypto.SigToPub(textHash(message), signature)
	if err != nil {
		return fmt.Errorf("pricesheet: entry for epoch %d: %w", e.EffectiveEpoch, err)
	}
	if recovered := crypto.PubkeyToAddress(*public); recovered != signer {
		return fmt.Errorf("pricesheet: entry for epoch %d is signed by %s, not %s", e.EffectiveEpoch, recovered, signer)
	}
	return nil
}

// textHash is EIP-191's personal message hash, which a hardware wallet's
// personal_sign signs.
func textHash(message string) []byte {
	return crypto.Keccak256(fmt.Appendf(nil, "\x19Ethereum Signed Message:\n%d%s", len(message), message))
}

// Message is the exact line the signer approves. It names everything the entry
// binds, in plain ASCII, with every number exact.
func (e *Entry) Message(chainID uint64, netuid uint16) (string, error) {
	if err := e.validate(); err != nil {
		return "", err
	}
	var line strings.Builder
	fmt.Fprintf(&line, "UR price sheet v1: chain %d, sn %d, from epoch %d, cap %s alpha per epoch", chainID, netuid, e.EffectiveEpoch, formatRao(new(big.Rat).SetUint64(e.EpochCapRao)))
	for _, tier := range e.Tiers {
		denominator := new(big.Int).SetUint64(tier.RateDenominator)
		gib := new(big.Rat).SetFrac(new(big.Int).SetUint64(tier.RateNumeratorRaoPerGiB), denominator)
		user := new(big.Rat).SetFrac(new(big.Int).SetUint64(tier.RateNumeratorRaoPerUser), denominator)
		fmt.Fprintf(&line, ", tier %s alpha %s per GiB %s per user", formatRao(new(big.Rat).SetUint64(tier.MinConvictionRao)), formatRao(gib), formatRao(user))
	}
	return line.String(), nil
}

func (e *Entry) validate() error {
	if e.EpochCapRao == 0 {
		return fmt.Errorf("pricesheet: entry for epoch %d has no cap", e.EffectiveEpoch)
	}
	if len(e.Tiers) == 0 || len(e.Tiers) > maxTiers {
		return fmt.Errorf("pricesheet: entry for epoch %d has %d tiers", e.EffectiveEpoch, len(e.Tiers))
	}
	if _, err := protocol.ValidateDepositTiers(e.Tiers); err != nil {
		return fmt.Errorf("pricesheet: entry for epoch %d: %w", e.EffectiveEpoch, err)
	}
	return nil
}

// DepositPolicy is base with this entry's schedule and cap in force. The fixed
// rules of the audit (lag, snapshot and mismatch action) stay base's. A schedule of
// all zeros is the zero-price mode, so it declares equal_demand.
func (e *Entry) DepositPolicy(base protocol.DepositPolicy) protocol.DepositPolicy {
	out := base
	out.Tiers = slices.Clone(e.Tiers)
	out.EpochCapRaoPerOperator = e.EpochCapRao
	out.TotalTestCampaignCapRao = max(base.TotalTestCampaignCapRao, e.EpochCapRao)
	out.Unit = protocol.DepositUnitRaoPerGiB
	for _, tier := range e.Tiers {
		if tier.RateNumeratorRaoPerUser != 0 {
			out.Unit = protocol.DepositUnitRaoPerGiBAndUser
		}
	}
	out.ZeroRateAction = ""
	if out.IsZeroPrice() {
		out.ZeroRateAction = protocol.DepositZeroRateEqualDemand
	}
	return out
}

// Effective is the deposit policy in force for an epoch: the signed entry's, or
// base when the sheet has none for it yet.
func Effective(sheet *Sheet, epoch uint64, base protocol.DepositPolicy) protocol.DepositPolicy {
	if entry := sheet.At(epoch); entry != nil {
		return entry.DepositPolicy(base)
	}
	return base
}

// formatRao writes a rao amount as an exact decimal of alpha.
func formatRao(rao *big.Rat) string {
	alpha := new(big.Rat).Quo(rao, new(big.Rat).SetInt(pow10(raoDecimals)))
	text := alpha.FloatString(36)
	text = strings.TrimRight(text, "0")
	return strings.TrimSuffix(text, ".")
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

var decimalText = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// parseDecimal reads a plain decimal scalar exactly, never through a float. YAML's
// other spellings of a number (1_000, 1e3, 0x3E8, +1) are refused.
func parseDecimal(node *yaml.Node) (*big.Rat, int, error) {
	if node.Kind != yaml.ScalarNode || node.Style != 0 || (node.Tag != "!!int" && node.Tag != "!!float") || !decimalText.MatchString(node.Value) {
		return nil, 0, fmt.Errorf("pricesheet: line %d: want a plain decimal number", node.Line)
	}
	value, ok := new(big.Rat).SetString(node.Value)
	if !ok {
		return nil, 0, fmt.Errorf("pricesheet: line %d: want a plain decimal number", node.Line)
	}
	decimals := 0
	if dot := strings.IndexByte(node.Value, '.'); dot >= 0 {
		decimals = len(node.Value) - dot - 1
	}
	return value, decimals, nil
}

// raoOf converts alpha to whole rao scaled by scale, refusing anything inexact or
// beyond uint64.
func raoOf(alpha *big.Rat, scale *big.Int) (uint64, error) {
	scaled := new(big.Rat).Mul(alpha, new(big.Rat).SetInt(new(big.Int).Mul(pow10(raoDecimals), scale)))
	if !scaled.IsInt() || !scaled.Num().IsUint64() {
		return 0, fmt.Errorf("pricesheet: %s alpha has no exact uint64 representation", alpha.FloatString(raoDecimals))
	}
	return scaled.Num().Uint64(), nil
}

func mappingPairs(node *yaml.Node) ([][2]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("pricesheet: line %d: want a mapping", node.Line)
	}
	pairs := make([][2]*yaml.Node, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		pairs = append(pairs, [2]*yaml.Node{node.Content[i], node.Content[i+1]})
	}
	return pairs, nil
}

// parseTiers reads a tier mapping with one denominator for the whole schedule: the
// power of ten that makes every rate a whole number of rao.
func parseTiers(node *yaml.Node) ([]protocol.DepositTier, error) {
	pairs, err := mappingPairs(node)
	if err != nil {
		return nil, err
	}
	type raw struct {
		threshold, gib, user *big.Rat
	}
	tiers := make([]raw, 0, len(pairs))
	decimals := 0
	for _, pair := range pairs {
		threshold, _, err := parseDecimal(pair[0])
		if err != nil {
			return nil, err
		}
		fields, err := mappingPairs(pair[1])
		if err != nil {
			return nil, err
		}
		tier := raw{threshold: threshold}
		for _, field := range fields {
			value, places, err := parseDecimal(field[1])
			if err != nil {
				return nil, err
			}
			decimals = max(decimals, places)
			switch {
			case field[0].Value == "alpha_per_gib" && tier.gib == nil:
				tier.gib = value
			case field[0].Value == "alpha_per_user" && tier.user == nil:
				tier.user = value
			default:
				return nil, fmt.Errorf("pricesheet: line %d: unexpected tier field %q", field[0].Line, field[0].Value)
			}
		}
		if tier.gib == nil || tier.user == nil {
			return nil, fmt.Errorf("pricesheet: line %d: a tier needs alpha_per_gib and alpha_per_user", pair[0].Line)
		}
		tiers = append(tiers, tier)
	}
	scale := pow10(max(0, decimals-raoDecimals))
	if !scale.IsUint64() {
		return nil, errors.New("pricesheet: too many decimals")
	}
	out := make([]protocol.DepositTier, len(tiers))
	for i, tier := range tiers {
		minimum, err := raoOf(tier.threshold, big.NewInt(1))
		if err != nil {
			return nil, err
		}
		gib, err := raoOf(tier.gib, scale)
		if err != nil {
			return nil, err
		}
		user, err := raoOf(tier.user, scale)
		if err != nil {
			return nil, err
		}
		out[i] = protocol.DepositTier{MinConvictionRao: minimum, RateNumeratorRaoPerGiB: gib, RateNumeratorRaoPerUser: user, RateDenominator: scale.Uint64()}
	}
	return out, nil
}

// Parse reads a sheet strictly: numbers only as plain decimals, no unknown or
// repeated fields in anything signed. It does not check signatures; Verify does.
func Parse(raw []byte) (*Sheet, error) {
	if len(raw) > maxSheetBytes {
		return nil, fmt.Errorf("pricesheet: %d bytes exceeds %d", len(raw), maxSheetBytes)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("pricesheet: not YAML: %w", err)
	}
	if err := decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("pricesheet: more than one YAML document")
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("pricesheet: not a YAML document")
	}
	pairs, err := mappingPairs(document.Content[0])
	if err != nil {
		return nil, err
	}
	sheet := &Sheet{}
	seen := map[string]bool{}
	for _, pair := range pairs {
		key := pair[0].Value
		if seen[key] {
			return nil, fmt.Errorf("pricesheet: line %d: repeated field %q", pair[0].Line, key)
		}
		seen[key] = true
		switch key {
		case "sn":
			if pair[1].Tag != "!!int" {
				return nil, fmt.Errorf("pricesheet: line %d: sn is not an integer", pair[1].Line)
			}
			netuid, err := strconv.ParseUint(pair[1].Value, 10, 16)
			if err != nil || netuid == 0 {
				return nil, fmt.Errorf("pricesheet: line %d: bad sn", pair[1].Line)
			}
			sheet.Netuid = uint16(netuid)
		case "tiered_per_block_demand_deposits":
			if sheet.Mirror, err = parseTiers(pair[1]); err != nil {
				return nil, err
			}
		case "signed_schedule":
			if sheet.Entries, err = parseSchedule(pair[1]); err != nil {
				return nil, err
			}
		}
	}
	if sheet.Netuid == 0 {
		return nil, errors.New("pricesheet: the sheet names no sn")
	}
	return sheet, nil
}

func parseSchedule(node *yaml.Node) ([]Entry, error) {
	pairs, err := mappingPairs(node)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(pairs))
	for _, pair := range pairs {
		if pair[0].Tag != "!!int" || len(pair[0].Value) > maxUint64Text {
			return nil, fmt.Errorf("pricesheet: line %d: an effective epoch must be an integer", pair[0].Line)
		}
		epoch, err := strconv.ParseUint(pair[0].Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("pricesheet: line %d: an effective epoch must be an integer", pair[0].Line)
		}
		if len(entries) > 0 && epoch <= entries[len(entries)-1].EffectiveEpoch {
			return nil, fmt.Errorf("pricesheet: line %d: effective epochs must increase", pair[0].Line)
		}
		fields, err := mappingPairs(pair[1])
		if err != nil {
			return nil, err
		}
		entry := Entry{EffectiveEpoch: epoch}
		seen := map[string]bool{}
		for _, field := range fields {
			key := field[0].Value
			if seen[key] {
				return nil, fmt.Errorf("pricesheet: line %d: repeated field %q", field[0].Line, key)
			}
			seen[key] = true
			switch key {
			case "epoch_cap_alpha":
				cap, _, err := parseDecimal(field[1])
				if err != nil {
					return nil, err
				}
				if entry.EpochCapRao, err = raoOf(cap, big.NewInt(1)); err != nil {
					return nil, err
				}
			case "signature":
				if field[1].Kind != yaml.ScalarNode {
					return nil, fmt.Errorf("pricesheet: line %d: bad signature", field[1].Line)
				}
				if entry.Signature, err = hex.DecodeString(strings.TrimPrefix(field[1].Value, "0x")); err != nil {
					return nil, fmt.Errorf("pricesheet: line %d: bad signature", field[1].Line)
				}
			case "tiers":
				if entry.Tiers, err = parseTiers(field[1]); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("pricesheet: line %d: unexpected field %q", field[0].Line, key)
			}
		}
		if err := entry.validate(); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// sameEntry reports equal prices and signature.
func sameEntry(a, b *Entry) bool {
	return a.EffectiveEpoch == b.EffectiveEpoch && a.EpochCapRao == b.EpochCapRao && slices.Equal(a.Tiers, b.Tiers) && bytes.Equal(a.Signature, b.Signature)
}

// Extends reports whether next keeps every entry of last, unchanged and in place, and
// adds only later ones. A signed entry is permanent, so a file that drops or changes
// one is a rollback, not news, and one that adds an entry before those would reprice
// epochs a reader has already decided. Parse requires epochs to increase, so keeping
// the leading entries is enough.
func Extends(next, last *Sheet) bool {
	if last == nil {
		return true
	}
	if len(next.Entries) < len(last.Entries) {
		return false
	}
	for i := range last.Entries {
		if !sameEntry(&last.Entries[i], &next.Entries[i]) {
			return false
		}
	}
	return true
}

// Store keeps the last verified sheet across restarts. Load returns nil bytes when
// there is none.
type Store interface {
	Load(ctx context.Context) ([]byte, error)
	Save(ctx context.Context, raw []byte) error
}

// Source fetches the published sheet and holds the last good copy, so an outage or
// a tampered file changes nothing: an unverified file is never used, and the
// schedule in force stays what was last verified.
type Source struct {
	url     string
	chainID uint64
	netuid  uint16
	signer  common.Address
	store   Store
	client  *http.Client
}

// NewSource builds a Source for a deployment's pinned signer. store may be nil, in
// which case nothing survives a restart.
func NewSource(url string, chainID uint64, netuid uint16, signer common.Address, store Store) *Source {
	return &Source{url: url, chainID: chainID, netuid: netuid, signer: signer, store: store, client: &http.Client{
		Timeout: 30 * time.Second,
		// A redirect would leave the published address.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// lastGood reads the stored sheet. A stored copy that no longer parses or verifies
// is no sheet at all, and the next verified one may replace it. A store that cannot
// be read may still hold a good copy, so nothing may overwrite it this time.
func (s *Source) lastGood(ctx context.Context) (last *Sheet, problem error, mayReplace bool) {
	if s.store == nil {
		return nil, nil, false
	}
	raw, err := s.store.Load(ctx)
	if err != nil {
		return nil, err, false
	}
	if len(raw) == 0 {
		return nil, nil, true
	}
	sheet, err := Parse(raw)
	if err != nil {
		return nil, err, true
	}
	if err := sheet.Verify(s.chainID, s.netuid, s.signer); err != nil {
		return nil, err, true
	}
	return sheet, nil, true
}

func (s *Source) fetch(ctx context.Context) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/yaml, text/plain")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxSheetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSheetBytes {
		return nil, fmt.Errorf("more than %d bytes", maxSheetBytes)
	}
	return raw, nil
}

// Refresh returns the sheet to use now. problem says what went wrong, for an alert:
// why the published file was not taken, in which case the sheet returned is the last
// good one, or that the stored copy could not be used. A nil sheet with a problem
// means no verified sheet is held and none could be fetched, so the price is unknown.
//
// A stored copy that is unreadable or corrupt does not stop the fetch: the published
// file, once verified, is taken as if nothing were held, as after a lost store.
func (s *Source) Refresh(ctx context.Context) (sheet *Sheet, problem error) {
	last, stored, mayReplace := s.lastGood(ctx)
	report := func(err error) error {
		switch {
		case stored == nil:
			return fmt.Errorf("pricesheet: %w", err)
		case err == nil:
			return fmt.Errorf("pricesheet: the stored sheet: %w", stored)
		default:
			return fmt.Errorf("pricesheet: %w; the stored sheet: %v", err, stored)
		}
	}
	raw, err := s.fetch(ctx)
	if err != nil {
		return last, report(err)
	}
	next, err := Parse(raw)
	if err != nil {
		return last, report(err)
	}
	if err := next.Verify(s.chainID, s.netuid, s.signer); err != nil {
		return last, report(err)
	}
	if !Extends(next, last) {
		return last, report(errors.New("the published sheet drops or changes a signed entry, or adds one before it"))
	}
	if mayReplace {
		if err := s.store.Save(ctx, raw); err != nil {
			return next, report(fmt.Errorf("save the sheet: %w", err))
		}
	}
	if stored != nil {
		return next, report(nil)
	}
	return next, nil
}
