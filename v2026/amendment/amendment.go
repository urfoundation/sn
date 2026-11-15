// Package amendment reads the signed settlement amendments published at
// https://ur.xyz/amendments.yml, beside the price sheet. An amendment changes how
// settlement counts from an epoch on, for one network operator, without a successor
// policy: the server resolves payout consents in a domain that includes the policy
// hash, so a new policy would leave every consent outside the new epochs' domain
// (CAPTURETHEFLAG.md §2.4). The server, the validator and snclaim read the same
// entries, so all three apply the same rules to an epoch.
//
// The file itself is not trusted. Only its signed entries count, each a line of
// plain text signed by the price sheet's pinned key (WHITEPAPER.md §7.1), so a
// person signing on a hardware wallet reads exactly what they approve:
//
//	UR settlement amendment v1: chain 964, sn 25, no 1, from epoch 8, intra-platform and intra-network extenders, units egress prefix, free and paid, zeta 1
//	UR settlement amendment v2: chain 964, sn 25, no 1, from epoch 8, capture the flag, ctf pool 4294967297, carry 0x…, shares pool 4/10 ctf 1/10 head 5/10, head cedes to pool, slots 5 providers 5 extenders, countries iso 3166-1 249, presence half epoch, units egress prefix, draw drand quicknet 60s after census
//
// Version 1 is the extender pool (WHITEPAPER.md §8.6.7), version 2 capture the flag
// (CAPTURETHEFLAG.md §2.4). A line states every term it binds, and the terms the
// owner decided are part of its version: a line that differs in any of them is not a
// line of that version, so a change to one is a new version, with readers built for
// it. What a line varies is its deployment (chain, sn), its network operator (no),
// its first epoch (from epoch) and, in version 2, the carry contract.
//
// An entry applies from its first epoch until a later entry of the same kind for the
// same operator takes effect. Where no signed entry of a kind is in force, the
// deployment's configuration may stand in, and only there: configuration is the
// fallback for an amendment whose signing cannot happen before its first epoch
// starts (WHITEPAPER.md §8.6.7).
package amendment

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/demand/pricesheet"
)

const (
	// URL is where the file is published, beside the price sheet.
	URL = "https://ur.xyz/amendments.yml"

	// linePrefix starts every line. Its version and ": " follow, then the clauses.
	linePrefix      = "UR settlement amendment v"
	clauseSeparator = ", "
	maxLineBytes    = 512
	maxUint64Text   = 20

	// ctfPoolBase puts an operator's CTF pool above every operator id, which stays
	// below 2^32 (CAPTURETHEFLAG.md §3.1).
	ctfPoolBase uint64 = 1 << 32
)

// SignerFor is the pinned signer of a deployment's amendments: the price sheet's,
// the sole owner of the Safe that owns the coordinator. A deployment with none has
// no published amendments, and only its configuration can name one.
func SignerFor(chainID uint64, netuid uint16) (common.Address, bool) {
	return pricesheet.SignerFor(chainID, netuid)
}

// CtfPoolID is network operator noID's CTF pool, the vault2 pool 2^32 + noID
// (CAPTURETHEFLAG.md §3.1), and false for a noID that is no operator id, which is 1
// to 2^32 - 1.
func CtfPoolID(noID uint64) (uint64, bool) {
	if noID == 0 || noID >= ctfPoolBase {
		return 0, false
	}
	return ctfPoolBase + noID, true
}

// CtfNoID is the network operator whose CTF pool poolID is, and false for a pool id
// that is no operator's CTF pool.
func CtfNoID(poolID uint64) (uint64, bool) {
	if poolID <= ctfPoolBase || poolID >= 2*ctfPoolBase {
		return 0, false
	}
	return poolID - ctfPoolBase, true
}

// Version is a line format, the number after "v" in its line.
type Version uint8

const (
	// V1 amends settlement with the extender pool (WHITEPAPER.md §8.6).
	V1 Version = 1
	// V2 amends it with capture the flag (CAPTURETHEFLAG.md).
	V2 Version = 2
)

// Kind is what an amendment changes. An entry stays in force until a later entry of
// the same kind for the same operator takes effect, and leaves other kinds alone.
type Kind uint8

const (
	// KindExtenders is the extender pool, stated by version 1 lines.
	KindExtenders Kind = 1
	// KindCaptureTheFlag is capture the flag, stated by version 2 lines.
	KindCaptureTheFlag Kind = 2
)

func (k Kind) String() string {
	switch k {
	case KindExtenders:
		return "the extender pool"
	case KindCaptureTheFlag:
		return "capture the flag"
	}
	return fmt.Sprintf("kind %d", uint8(k))
}

// Units names how miners group into units.
type Units string

// UnitsEgressPrefix groups by egress prefix: the /29 of an IPv4 address, else the
// /48 of an IPv6 one, keyed as a head unit is (WHITEPAPER.md §8.4).
const UnitsEgressPrefix Units = "egress prefix"

// Ratio is an exact fraction, Num/Den.
type Ratio struct {
	Num, Den uint64
}

// ExtenderTerms are what a version 1 line binds: the extender pool of WHITEPAPER.md
// §8.6.2 under the owner's decisions of §8.6.8.
type ExtenderTerms struct {
	// IntraPlatform and IntraNetwork are the traffic the pool's Z counts: contracts
	// with a platform endpoint, and same-network contracts (decision 6).
	IntraPlatform, IntraNetwork bool
	// Units groups extenders. Each unit counts once (decision 1).
	Units Units
	// FreeAndPaid counts free bytes toward Z as paid ones are counted (decision 2).
	FreeAndPaid bool
	// Zeta caps Z at ζ times the epoch's eligible provider usage (decision 9).
	Zeta Ratio
}

// Shares divide the providers' share μ over one denominator (CAPTURETHEFLAG.md
// §2.1).
type Shares struct {
	Pool, Ctf, Head Ratio
}

// CtfTerms are what a version 2 line binds: capture the flag as CAPTURETHEFLAG.md
// designs it under the owner's decisions of §12.1.
type CtfTerms struct {
	// CtfPool is the operator's CTF pool, the vault2 pool 2^32 + no (§3.1).
	CtfPool uint64
	// Carry is the carry contract, whose coldkey takes each root's carry (§4.6).
	Carry common.Address
	// Shares split the providers' share (§2.1, decision 2).
	Shares Shares
	// HeadCedesToPool gives the head's share to the provider pools alone while no
	// top-level miner is weighted, so the CTF pools keep exactly theirs (§2.1,
	// decision 3).
	HeadCedesToPool bool
	// ProviderSlots and ExtenderSlots are each country's slots per role (§4.1,
	// decision 4).
	ProviderSlots, ExtenderSlots uint64
	// Countries is the size of the country list, every ISO 3166-1 alpha-2 code
	// (§4.1, decision 9).
	Countries uint64
	// Presence is the part of an epoch's samples a candidate must be present in
	// (§4.2, decision 6).
	Presence Ratio
	// Units groups candidates into tickets (§4.2, decision 10).
	Units Units
	// DrawDelay is how long after the census commitment's block the draw's drand
	// quicknet round must be (§4.4).
	DrawDelay time.Duration
}

// The words each version states after its operator and first epoch, and the terms
// they mean. Both are the owner's decisions; Line writes the words, and an entry's
// terms must be exactly these.
var (
	extenderClauses = []string{
		"intra-platform and intra-network extenders",
		"units egress prefix",
		"free and paid",
		"zeta 1",
	}
	// The clauses of a version 2 line after its carry contract.
	ctfClauses = []string{
		"shares pool 4/10 ctf 1/10 head 5/10",
		"head cedes to pool",
		"slots 5 providers 5 extenders",
		"countries iso 3166-1 249",
		"presence half epoch",
		"units egress prefix",
		"draw drand quicknet 60s after census",
	}
)

func extenderTerms() ExtenderTerms {
	return ExtenderTerms{IntraPlatform: true, IntraNetwork: true, Units: UnitsEgressPrefix, FreeAndPaid: true, Zeta: Ratio{Num: 1, Den: 1}}
}

func ctfTerms(noID uint64, carry common.Address) CtfTerms {
	return CtfTerms{
		CtfPool:         ctfPoolBase + noID,
		Carry:           carry,
		Shares:          Shares{Pool: Ratio{Num: 4, Den: 10}, Ctf: Ratio{Num: 1, Den: 10}, Head: Ratio{Num: 5, Den: 10}},
		HeadCedesToPool: true,
		ProviderSlots:   5,
		ExtenderSlots:   5,
		Countries:       249,
		Presence:        Ratio{Num: 1, Den: 2},
		Units:           UnitsEgressPrefix,
		DrawDelay:       60 * time.Second,
	}
}

// Entry is one amendment: the terms its line states and, once signed, the signature
// over that line.
type Entry struct {
	Version Version
	ChainID uint64
	Netuid  uint16
	// NoID is the network operator the amendment applies to.
	NoID uint64
	// FromEpoch is the first epoch the amendment covers.
	FromEpoch uint64
	// Extender holds a version 1 line's terms, and is nil for any other.
	Extender *ExtenderTerms
	// Ctf holds a version 2 line's terms, and is nil for any other.
	Ctf *CtfTerms
	// Signature is the pinned signer's EIP-191 signature of the line, as a hardware
	// wallet returns it. A configured entry has none.
	Signature []byte
	// configured marks an entry of a Fallback.
	configured bool
}

// NewExtenders is a version 1 entry, unsigned.
func NewExtenders(chainID uint64, netuid uint16, noID, fromEpoch uint64) *Entry {
	terms := extenderTerms()
	return &Entry{Version: V1, ChainID: chainID, Netuid: netuid, NoID: noID, FromEpoch: fromEpoch, Extender: &terms}
}

// NewCaptureTheFlag is a version 2 entry, unsigned.
func NewCaptureTheFlag(chainID uint64, netuid uint16, noID, fromEpoch uint64, carry common.Address) *Entry {
	terms := ctfTerms(noID, carry)
	return &Entry{Version: V2, ChainID: chainID, Netuid: netuid, NoID: noID, FromEpoch: fromEpoch, Ctf: &terms}
}

// Kind is what the entry amends, zero for a version this package does not read.
func (e *Entry) Kind() Kind {
	switch e.Version {
	case V1:
		return KindExtenders
	case V2:
		return KindCaptureTheFlag
	}
	return 0
}

// Configured reports whether the entry is a configured one: File.At answered with it
// from its fallback, because no signed entry of its kind is in force. Every other
// answer of File.At is a signed entry that Verify checked.
func (e *Entry) Configured() bool {
	return e.configured
}

func (e *Entry) String() string {
	return fmt.Sprintf("the v%d entry from epoch %d for no %d", e.Version, e.FromEpoch, e.NoID)
}

// clone copies the entry, so a reader's answer shares nothing with the file.
func (e *Entry) clone() *Entry {
	out := *e
	if e.Extender != nil {
		terms := *e.Extender
		out.Extender = &terms
	}
	if e.Ctf != nil {
		terms := *e.Ctf
		out.Ctf = &terms
	}
	out.Signature = slices.Clone(e.Signature)
	return &out
}

func (e *Entry) validate() error {
	switch {
	case e.ChainID == 0:
		return errors.New("amendment: an entry names no chain")
	case e.Netuid == 0:
		return errors.New("amendment: an entry names no subnet")
	case e.NoID == 0 || e.NoID >= ctfPoolBase:
		return fmt.Errorf("amendment: operator %d is not an operator id, which is 1 to %d", e.NoID, ctfPoolBase-1)
	}
	switch e.Version {
	case V1:
		if e.Extender == nil || e.Ctf != nil {
			return fmt.Errorf("amendment: %s holds other terms than the extender pool's", e)
		}
		if *e.Extender != extenderTerms() {
			return fmt.Errorf("amendment: %s has terms a v1 line does not state: %+v", e, *e.Extender)
		}
	case V2:
		if e.Ctf == nil || e.Extender != nil {
			return fmt.Errorf("amendment: %s holds other terms than capture the flag's", e)
		}
		if e.Ctf.Carry == (common.Address{}) {
			return fmt.Errorf("amendment: %s names no carry contract", e)
		}
		if *e.Ctf != ctfTerms(e.NoID, e.Ctf.Carry) {
			return fmt.Errorf("amendment: %s has terms a v2 line does not state: %+v", e, *e.Ctf)
		}
	default:
		return fmt.Errorf("amendment: version %d is not a line format this release reads", e.Version)
	}
	return nil
}

// clauses are the entry's line after its version, one clause per term.
func (e *Entry) clauses() []string {
	clauses := []string{
		"chain " + strconv.FormatUint(e.ChainID, 10),
		"sn " + strconv.FormatUint(uint64(e.Netuid), 10),
		"no " + strconv.FormatUint(e.NoID, 10),
		"from epoch " + strconv.FormatUint(e.FromEpoch, 10),
	}
	if e.Version == V1 {
		return append(clauses, extenderClauses...)
	}
	clauses = append(clauses,
		"capture the flag",
		"ctf pool "+strconv.FormatUint(e.Ctf.CtfPool, 10),
		"carry "+e.Ctf.Carry.Hex(),
	)
	return append(clauses, ctfClauses...)
}

// Line is the exact line the signer approves. It names everything the entry binds,
// in plain ASCII, with every number exact and the carry contract in its EIP-55
// checksum form.
func (e *Entry) Line() (string, error) {
	if err := e.validate(); err != nil {
		return "", err
	}
	return linePrefix + strconv.FormatUint(uint64(e.Version), 10) + ": " + strings.Join(e.clauses(), clauseSeparator), nil
}

var (
	wholeText   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	addressText = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
)

// parseWhole reads a plain whole number: no sign, no leading zero, no other base.
func parseWhole(text string) (uint64, error) {
	if len(text) > maxUint64Text || !wholeText.MatchString(text) {
		return 0, fmt.Errorf("%q is not a plain whole number", text)
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is out of range", text)
	}
	return value, nil
}

// parseCarry reads the carry contract in its EIP-55 checksum form, so that a
// mistyped address fails its checksum here, before anyone signs it.
func parseCarry(text string) (common.Address, error) {
	if !addressText.MatchString(text) {
		return common.Address{}, fmt.Errorf("%q is not an address, 0x and 40 hex digits", text)
	}
	address := common.HexToAddress(text)
	if address == (common.Address{}) {
		return common.Address{}, errors.New("the carry contract is the zero address")
	}
	if address.Hex() != text {
		return common.Address{}, fmt.Errorf("%s is not in its EIP-55 checksum form: check it digit by digit against the carry contract's address", text)
	}
	return address, nil
}

// ParseLine reads a line strictly: exactly the text Line writes for its terms, so a
// line that differs from it in any character is refused. It does not check a
// signature; Verify does.
func ParseLine(line string) (*Entry, error) {
	if len(line) == 0 || len(line) > maxLineBytes {
		return nil, fmt.Errorf("amendment: a line is 1 to %d bytes, got %d", maxLineBytes, len(line))
	}
	for i := 0; i < len(line); i++ {
		if line[i] < 0x20 || line[i] > 0x7e {
			return nil, fmt.Errorf("amendment: byte %d of the line, %#x, is not printable ASCII", i, line[i])
		}
	}
	rest, ok := strings.CutPrefix(line, linePrefix)
	if !ok {
		return nil, fmt.Errorf("amendment: a line starts %q", linePrefix+"N: ")
	}
	versionText, rest, ok := strings.Cut(rest, ": ")
	if !ok {
		return nil, fmt.Errorf("amendment: a line starts %q", linePrefix+"N: ")
	}
	version, err := parseWhole(versionText)
	if err != nil {
		return nil, fmt.Errorf("amendment: the version: %w", err)
	}
	entry := &Entry{Version: Version(min(version, 255))}
	var count int
	switch entry.Version {
	case V1:
		count = 4 + len(extenderClauses)
	case V2:
		count = 7 + len(ctfClauses)
	default:
		return nil, fmt.Errorf("amendment: version %s is not a line format this release reads", versionText)
	}
	clauses := strings.Split(rest, clauseSeparator)
	if len(clauses) != count {
		return nil, fmt.Errorf("amendment: a v%d line has %d clauses after its version, separated by %q; this one has %d", entry.Version, count, clauseSeparator, len(clauses))
	}
	// Each value follows its words in a clause of its own.
	value := func(index int, words string, read func(string) error) error {
		text, ok := strings.CutPrefix(clauses[index], words)
		if !ok {
			return fmt.Errorf("amendment: clause %d of a v%d line is %q, want %q and its value", index+1, entry.Version, clauses[index], words)
		}
		if err := read(text); err != nil {
			return fmt.Errorf("amendment: clause %d, %q: %w", index+1, clauses[index], err)
		}
		return nil
	}
	whole := func(target *uint64, low, high uint64) func(string) error {
		return func(text string) error {
			number, err := parseWhole(text)
			if err != nil {
				return err
			}
			if number < low || number > high {
				return fmt.Errorf("%d is not %d to %d", number, low, high)
			}
			*target = number
			return nil
		}
	}
	var netuid uint64
	var carry common.Address
	reads := []error{
		value(0, "chain ", whole(&entry.ChainID, 1, ^uint64(0))),
		value(1, "sn ", whole(&netuid, 1, 1<<16-1)),
		value(2, "no ", whole(&entry.NoID, 1, ctfPoolBase-1)),
		value(3, "from epoch ", whole(&entry.FromEpoch, 0, ^uint64(0))),
	}
	entry.Netuid = uint16(netuid)
	if entry.Version == V2 {
		reads = append(reads, value(6, "carry ", func(text string) (err error) {
			carry, err = parseCarry(text)
			return err
		}))
	}
	if err := errors.Join(reads...); err != nil {
		return nil, err
	}
	switch entry.Version {
	case V1:
		terms := extenderTerms()
		entry.Extender = &terms
	case V2:
		terms := ctfTerms(entry.NoID, carry)
		entry.Ctf = &terms
	}
	// Every other clause is words the version fixes, or the operator's own CTF pool.
	// Line writes all of it, so the line must be what Line writes.
	for index, want := range entry.clauses() {
		if clauses[index] != want {
			return nil, fmt.Errorf("amendment: clause %d of a v%d line is %q, want %q", index+1, entry.Version, clauses[index], want)
		}
	}
	if written, err := entry.Line(); err != nil || written != line {
		return nil, fmt.Errorf("amendment: the line does not read back as written: %v", err)
	}
	return entry, nil
}

// textHash is EIP-191's personal message hash, which a hardware wallet's
// personal_sign signs.
func textHash(message string) []byte {
	return crypto.Keccak256(fmt.Appendf(nil, "\x19Ethereum Signed Message:\n%d%s", len(message), message))
}

// recoverable checks a signature in the one form a hardware wallet returns it,
// r ‖ s ‖ v with v 27 or 28 and s in the lower half of the curve order, so an entry
// has exactly one signature. It returns the signature with v as crypto takes it.
func recoverable(signature []byte) ([]byte, error) {
	if len(signature) != crypto.SignatureLength {
		return nil, fmt.Errorf("a %d-byte signature, not %d", len(signature), crypto.SignatureLength)
	}
	v := signature[crypto.RecoveryIDOffset]
	if v != 27 && v != 28 {
		return nil, fmt.Errorf("recovery byte %d, not 27 or 28", v)
	}
	r, s := new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:64])
	if !crypto.ValidateSignatureValues(v-27, r, s, true) {
		return nil, errors.New("r or s is out of range, or s is in the upper half of the curve order")
	}
	out := slices.Clone(signature)
	out[crypto.RecoveryIDOffset] = v - 27
	return out, nil
}

// Verify checks that the entry is a valid amendment for this deployment, signed by
// signer over its line.
func (e *Entry) Verify(chainID uint64, netuid uint16, signer common.Address) error {
	line, err := e.Line()
	if err != nil {
		return err
	}
	if e.ChainID != chainID || e.Netuid != netuid {
		return fmt.Errorf("amendment: %s is for chain %d sn %d, not chain %d sn %d", e, e.ChainID, e.Netuid, chainID, netuid)
	}
	if signer == (common.Address{}) {
		return fmt.Errorf("amendment: %s: no signer to verify it against", e)
	}
	signature, err := recoverable(e.Signature)
	if err != nil {
		return fmt.Errorf("amendment: %s: %w", e, err)
	}
	public, err := crypto.SigToPub(textHash(line), signature)
	if err != nil {
		return fmt.Errorf("amendment: %s: %w", e, err)
	}
	if recovered := crypto.PubkeyToAddress(*public); recovered != signer {
		return fmt.Errorf("amendment: %s is signed by %s, not %s", e, recovered, signer)
	}
	return nil
}

// Sign signs the entry's line with key, as personal_sign on a hardware wallet does,
// and sets its signature.
func (e *Entry) Sign(key *ecdsa.PrivateKey) error {
	line, err := e.Line()
	if err != nil {
		return err
	}
	signature, err := crypto.Sign(textHash(line), key)
	if err != nil {
		return fmt.Errorf("amendment: sign %s: %w", e, err)
	}
	signature[crypto.RecoveryIDOffset] += 27
	e.Signature = signature
	return nil
}
