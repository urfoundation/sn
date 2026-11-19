package amendment

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

const maxFileBytes = 256 << 10

// signatureText is a signature as the file holds it: 0x and 65 bytes of lowercase hex.
var signatureText = regexp.MustCompile(`^0x[0-9a-f]{130}$`)

var (
	// ErrUnknown is At's answer without a file. What is in force is then unknown, and
	// configuration does not stand in for it: a signed entry the reader cannot see may
	// be in force.
	ErrUnknown = errors.New("amendment: no verified amendments file is held, so the amendments in force are unknown")
	// ErrUnverified is At's answer for a file Verify has not passed.
	ErrUnverified = errors.New("amendment: the file has not been verified")
)

// File is a parsed amendments file: its signed entries in file order. Only ParseFile
// and ParseVerified make one with entries, and nothing changes them after, so the
// entries Verify checked are the entries At applies.
type File struct {
	netuid  uint16
	entries []Entry
	// verified is set by Verify once every entry has checked out, and cleared by a
	// Verify that fails. At answers only while it is set, so no reader can apply an
	// entry nobody verified.
	verified bool
}

// Unpublished is the file of a deployment with no pinned signer, which publishes no
// amendments: none is signed, so only its configuration can put one in force. A
// deployment with a pinned signer has no such file. What is in force there comes from
// its verified file, and without one it is unknown, never configuration alone.
func Unpublished(chainID uint64, netuid uint16) (*File, error) {
	if _, pinned := SignerFor(chainID, netuid); pinned {
		return nil, fmt.Errorf("amendment: chain %d sn %d publishes signed amendments, so only its verified file says what is in force", chainID, netuid)
	}
	return &File{netuid: netuid, verified: true}, nil
}

// Netuid is the subnet the file names.
func (f *File) Netuid() uint16 {
	if f == nil {
		return 0
	}
	return f.netuid
}

// Entries are the file's entries in file order, copied. They are what the file says,
// verified or not; only At says what is in force.
func (f *File) Entries() []Entry {
	if f == nil {
		return nil
	}
	out := make([]Entry, len(f.entries))
	for i := range f.entries {
		out[i] = *f.entries[i].clone()
	}
	return out
}

// Verify checks that the file is for this subnet, that its entries are in the order
// a file lists them, and that every entry is for this deployment and signed by signer.
func (f *File) Verify(chainID uint64, netuid uint16, signer common.Address) error {
	if f == nil {
		return errors.New("amendment: no file")
	}
	f.verified = false
	if f.netuid != netuid {
		return fmt.Errorf("amendment: the file is for sn %d, not sn %d", f.netuid, netuid)
	}
	if err := checkOrder(f.entries, true); err != nil {
		return fmt.Errorf("amendment: %w", err)
	}
	for i := range f.entries {
		if err := f.entries[i].Verify(chainID, netuid, signer); err != nil {
			return err
		}
	}
	f.verified = true
	return nil
}

// ParseVerified is ParseFile and Verify: the file, verified for this deployment and
// signer, or an error.
func ParseVerified(raw []byte, chainID uint64, netuid uint16, signer common.Address) (*File, error) {
	file, err := ParseFile(raw)
	if err != nil {
		return nil, err
	}
	if err := file.Verify(chainID, netuid, signer); err != nil {
		return nil, err
	}
	return file, nil
}

// InForce is the amendments in force for one network operator in one epoch, nil for
// a kind with none in force.
type InForce struct {
	// Extenders is the extender pool's entry; its FromEpoch is E_x.
	Extenders *Entry
	// CaptureTheFlag is capture the flag's entry; its FromEpoch is E_c.
	CaptureTheFlag *Entry
}

// At is the amendments in force for operator noID in epoch: for each kind, the
// signed entry with the latest first epoch at or before it. Only for a kind with no
// signed entry in force does fallback, the deployment's configuration, stand in, as
// WHITEPAPER.md §8.6.7 keeps configuration only for an amendment whose signing
// cannot happen before its first epoch. fallback may be nil.
//
// The file must be verified: a Source returns only verified files, and so does
// ParseVerified. A nil file is ErrUnknown, and one not verified ErrUnverified.
func (f *File) At(noID, epoch uint64, fallback *Fallback) (InForce, error) {
	if f == nil {
		return InForce{}, ErrUnknown
	}
	if !f.verified {
		return InForce{}, ErrUnverified
	}
	var configured []Entry
	if fallback != nil {
		configured = fallback.entries
	}
	at := func(kind Kind) *Entry {
		if entry := inForce(f.entries, kind, noID, epoch); entry != nil {
			return entry
		}
		return inForce(configured, kind, noID, epoch)
	}
	return InForce{Extenders: at(KindExtenders), CaptureTheFlag: at(KindCaptureTheFlag)}, nil
}

// inForce is the entry of kind for operator noID with the latest first epoch at or
// before epoch, copied, nil for none.
func inForce(entries []Entry, kind Kind, noID, epoch uint64) *Entry {
	var found *Entry
	for i := range entries {
		entry := &entries[i]
		if entry.Kind() == kind && entry.NoID == noID && entry.FromEpoch <= epoch && (found == nil || entry.FromEpoch > found.FromEpoch) {
			found = entry
		}
	}
	if found == nil {
		return nil
	}
	return found.clone()
}

// checkOrder requires the entries of one kind for one operator to take effect at
// increasing first epochs. listed adds a file's order: no entry takes effect before
// one listed ahead of it, of any kind or operator, so an entry added to a file never
// takes effect before one a reader holds (WHITEPAPER.md §7.1).
func checkOrder(entries []Entry, listed bool) error {
	type series struct {
		kind Kind
		noID uint64
	}
	latest := map[series]uint64{}
	var newest uint64
	for i := range entries {
		entry := &entries[i]
		if listed && entry.FromEpoch < newest {
			return fmt.Errorf("%s takes effect before an entry listed ahead of it, from epoch %d: a file lists its entries in the order they take effect", entry, newest)
		}
		newest = max(newest, entry.FromEpoch)
		key := series{entry.Kind(), entry.NoID}
		if previous, ok := latest[key]; ok && entry.FromEpoch <= previous {
			return fmt.Errorf("%s is not after the entry of its kind for its operator from epoch %d", entry, previous)
		}
		latest[key] = entry.FromEpoch
	}
	return nil
}

// Fallback is a deployment's configured amendments: unsigned lines its configuration
// states. For the extender pool (v1 lines) they are the server's st.yml amendments
// block and the file each validator's run flag --amendments-fallback names
// (VALIDATOR.md §11.6); for capture the flag (v2 lines), the server's st.yml
// ctf.amendment_lines and the validator release configuration's amendment_fallback
// (VALIDATOR.md §11.5). They stand in only for a kind with no signed entry in force
// (File.At): configuration is the fallback for an amendment whose signing cannot
// happen before its first epoch starts (WHITEPAPER.md §8.6.7), never for a file that
// cannot be read.
//
// A configured line stands in for every epoch before the first signed entry of its
// kind for its operator. Remove it once a signed entry covers the epochs it was for:
// left in place, it would still put the amendment in force before a later signed
// start. Postponing a start therefore means removing the line from every reader's
// configuration alike.
type Fallback struct {
	entries []Entry
}

// ParseFallback reads a deployment's configured lines. Each must be a valid line for
// this deployment, and no two of one kind for one operator may share a first epoch.
// No lines is no fallback.
func ParseFallback(chainID uint64, netuid uint16, lines []string) (*Fallback, error) {
	fallback := &Fallback{entries: make([]Entry, 0, len(lines))}
	for i, line := range lines {
		entry, err := ParseLine(line)
		if err != nil {
			return nil, fmt.Errorf("amendment: configured line %d: %s", i+1, strings.TrimPrefix(err.Error(), "amendment: "))
		}
		if entry.ChainID != chainID || entry.Netuid != netuid {
			return nil, fmt.Errorf("amendment: configured line %d is for chain %d sn %d, not chain %d sn %d", i+1, entry.ChainID, entry.Netuid, chainID, netuid)
		}
		entry.configured = true
		fallback.entries = append(fallback.entries, *entry)
	}
	if err := checkOrder(fallback.entries, false); err != nil {
		return nil, fmt.Errorf("amendment: configured: %w", err)
	}
	return fallback, nil
}

// Entries are the configured entries, copied, for a reader to log what it would fall
// back to.
func (f *Fallback) Entries() []Entry {
	if f == nil {
		return nil
	}
	out := make([]Entry, len(f.entries))
	for i := range f.entries {
		out[i] = *f.entries[i].clone()
	}
	return out
}

func mappingPairs(node *yaml.Node) ([][2]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("amendment: line %d: want a mapping", node.Line)
	}
	pairs := make([][2]*yaml.Node, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		pairs = append(pairs, [2]*yaml.Node{node.Content[i], node.Content[i+1]})
	}
	return pairs, nil
}

// verbatim reports whether node is a double-quoted scalar written in the file as its
// value in quotes, on one line and with no escape. The file then reads exactly as what
// was signed.
func verbatim(lines [][]byte, node *yaml.Node) bool {
	if node.Kind != yaml.ScalarNode || node.Style != yaml.DoubleQuotedStyle || node.Line < 1 || node.Line > len(lines) || node.Column < 1 {
		return false
	}
	text := []rune(string(lines[node.Line-1]))
	want := []rune(`"` + node.Value + `"`)
	start := node.Column - 1
	return start+len(want) <= len(text) && string(text[start:start+len(want)]) == string(want)
}

func parseEntries(raw []byte, node *yaml.Node) ([]Entry, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("amendment: line %d: signed_amendments must be a list", node.Line)
	}
	lines := bytes.Split(raw, []byte{'\n'})
	entries := make([]Entry, 0, len(node.Content))
	for _, item := range node.Content {
		pairs, err := mappingPairs(item)
		if err != nil {
			return nil, err
		}
		var lineNode, signatureNode *yaml.Node
		for _, pair := range pairs {
			switch {
			case pair[0].Value == "line" && lineNode == nil:
				lineNode = pair[1]
			case pair[0].Value == "signature" && signatureNode == nil:
				signatureNode = pair[1]
			default:
				return nil, fmt.Errorf("amendment: line %d: unexpected or repeated field %q", pair[0].Line, pair[0].Value)
			}
		}
		if lineNode == nil || signatureNode == nil {
			return nil, fmt.Errorf("amendment: line %d: an entry has a line and a signature", item.Line)
		}
		if !verbatim(lines, lineNode) {
			return nil, fmt.Errorf("amendment: line %d: an entry's line is written in double quotes, on one line, exactly as signed", lineNode.Line)
		}
		entry, err := ParseLine(lineNode.Value)
		if err != nil {
			return nil, fmt.Errorf("amendment: line %d: %s", lineNode.Line, strings.TrimPrefix(err.Error(), "amendment: "))
		}
		if !verbatim(lines, signatureNode) || !signatureText.MatchString(signatureNode.Value) {
			return nil, fmt.Errorf("amendment: line %d: a signature is 0x and 65 bytes of lowercase hex, in double quotes", signatureNode.Line)
		}
		if entry.Signature, err = hex.DecodeString(signatureNode.Value[2:]); err != nil {
			return nil, fmt.Errorf("amendment: line %d: %w", signatureNode.Line, err)
		}
		entries = append(entries, *entry)
	}
	if err := checkOrder(entries, true); err != nil {
		return nil, fmt.Errorf("amendment: %w", err)
	}
	return entries, nil
}

var netuidText = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)

// ParseFile reads a file strictly: one YAML document naming its subnet, and every
// entry a valid line for that subnet, written exactly as signed, and its signature.
// Entries are listed in the order they take effect, and those of one kind for one
// operator at increasing first epochs. Top-level fields it does not use are passed
// over, as the price sheet's reader passes them over. It does not check signatures;
// Verify does.
func ParseFile(raw []byte) (*File, error) {
	if len(raw) > maxFileBytes {
		return nil, fmt.Errorf("amendment: %d bytes exceeds %d", len(raw), maxFileBytes)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("amendment: not YAML: %w", err)
	}
	if err := decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return nil, errors.New("amendment: more than one YAML document")
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, errors.New("amendment: not a YAML document")
	}
	pairs, err := mappingPairs(document.Content[0])
	if err != nil {
		return nil, err
	}
	file := &File{}
	seen := map[string]bool{}
	for _, pair := range pairs {
		key := pair[0].Value
		if seen[key] {
			return nil, fmt.Errorf("amendment: line %d: repeated field %q", pair[0].Line, key)
		}
		seen[key] = true
		switch key {
		case "sn":
			value := pair[1]
			if value.Kind != yaml.ScalarNode || value.Tag != "!!int" || value.Style != 0 || !netuidText.MatchString(value.Value) {
				return nil, fmt.Errorf("amendment: line %d: sn is a plain whole number", value.Line)
			}
			netuid, err := strconv.ParseUint(value.Value, 10, 16)
			if err != nil {
				return nil, fmt.Errorf("amendment: line %d: bad sn", value.Line)
			}
			file.netuid = uint16(netuid)
		case "signed_amendments":
			if file.entries, err = parseEntries(raw, pair[1]); err != nil {
				return nil, err
			}
		}
	}
	if file.netuid == 0 {
		return nil, errors.New("amendment: the file names no sn")
	}
	for i := range file.entries {
		if file.entries[i].Netuid != file.netuid {
			return nil, fmt.Errorf("amendment: %s is for sn %d, in a file for sn %d", &file.entries[i], file.entries[i].Netuid, file.netuid)
		}
	}
	return file, nil
}

// sameEntry reports the same line and signature.
func sameEntry(a, b *Entry) bool {
	lineA, errA := a.Line()
	lineB, errB := b.Line()
	return errA == nil && errB == nil && lineA == lineB && bytes.Equal(a.Signature, b.Signature)
}

// Extends reports whether next keeps every entry of last, unchanged and in place,
// and adds only later ones. A signed entry is permanent, so a file that drops or
// changes one is a rollback, not news. A file lists its entries in the order they take
// effect, so an entry added after those a reader holds takes effect no earlier than
// any of them.
func Extends(next, last *File) bool {
	if last == nil {
		return true
	}
	if next == nil || len(next.entries) < len(last.entries) {
		return false
	}
	for i := range last.entries {
		if !sameEntry(&last.entries[i], &next.entries[i]) {
			return false
		}
	}
	return true
}

// Store keeps the last verified file across restarts. Load returns nil bytes when
// there is none.
type Store interface {
	Load(ctx context.Context) ([]byte, error)
	Save(ctx context.Context, raw []byte) error
}

// Source fetches the published file and holds the last good copy, so an outage or a
// tampered file changes nothing: an unverified file is never used, and the entries in
// force stay what was last verified. It holds that copy itself as well as in its
// store, so a store that is missing, unreadable or shared with an older writer cannot
// walk it back.
type Source struct {
	url     string
	chainID uint64
	netuid  uint16
	signer  common.Address
	store   Store
	client  *http.Client

	// One refresh at a time.
	refreshing sync.Mutex
	// held is the newest verified file this source has taken or read.
	held *File
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

// lastGood reads the stored file. A stored copy that no longer parses or verifies is
// no file at all, and the next verified one may replace it. A store that cannot be
// read may still hold a good copy, so nothing may overwrite it this time.
func (s *Source) lastGood(ctx context.Context) (last *File, problem error, mayReplace bool) {
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
	file, err := ParseVerified(raw, s.chainID, s.netuid, s.signer)
	if err != nil {
		return nil, err, true
	}
	return file, nil, true
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
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFileBytes {
		return nil, fmt.Errorf("more than %d bytes", maxFileBytes)
	}
	return raw, nil
}

// newer is whichever of a and b keeps every entry of the other, and false when
// neither does: two verified files that conflict, in which case a stays.
func newer(a, b *File) (*File, bool) {
	switch {
	case a == nil:
		return b, true
	case b == nil, Extends(a, b):
		return a, true
	case Extends(b, a):
		return b, true
	}
	return a, false
}

// Refresh returns the file to use now, verified. problem says what went wrong, for
// an alert: why the published file was not taken, in which case the file returned is
// the last good one, or that the stored copy could not be used. A nil file with a
// problem means no verified file is held and none could be fetched, so the
// amendments in force are unknown (ErrUnknown).
//
// The published file is taken only if it keeps every entry of both the file this
// source holds and the stored copy. A stored copy that is unreadable or corrupt does
// not stop the fetch: the published file, once verified, is taken as if nothing were
// stored, as after a lost store.
func (s *Source) Refresh(ctx context.Context) (file *File, problem error) {
	s.refreshing.Lock()
	defer s.refreshing.Unlock()
	stored, storedProblem, mayReplace := s.lastGood(ctx)
	var notes []string
	if storedProblem != nil {
		notes = append(notes, "the stored file: "+strings.TrimPrefix(storedProblem.Error(), "amendment: "))
	}
	held, agree := newer(s.held, stored)
	if !agree {
		notes = append(notes, "the stored file conflicts with the verified file held, which stays in force")
	}
	s.held = held
	report := func(err error) error {
		var parts []string
		if err != nil {
			parts = append(parts, strings.TrimPrefix(err.Error(), "amendment: "))
		}
		parts = append(parts, notes...)
		if len(parts) == 0 {
			return nil
		}
		return errors.New("amendment: " + strings.Join(parts, "; "))
	}
	raw, err := s.fetch(ctx)
	if err != nil {
		return held, report(err)
	}
	next, err := ParseVerified(raw, s.chainID, s.netuid, s.signer)
	if err != nil {
		return held, report(err)
	}
	if !Extends(next, held) || !Extends(next, stored) {
		return held, report(errors.New("the published file drops or changes a signed entry, or adds one before it"))
	}
	s.held = next
	if mayReplace && (stored == nil || len(next.entries) > len(stored.entries)) {
		if err := s.save(ctx, raw, next); err != nil {
			return next, report(err)
		}
	}
	return next, report(nil)
}

// save stores raw, which next was read from, unless the store has moved on since it
// was read, in another process sharing it, to a file next does not keep.
func (s *Source) save(ctx context.Context, raw []byte, next *File) error {
	stored, problem, mayReplace := s.lastGood(ctx)
	switch {
	case !mayReplace:
		return fmt.Errorf("the file was not saved: the store could not be read again: %v", problem)
	case Extends(next, stored):
	case Extends(stored, next):
		// A newer copy is already stored.
		return nil
	default:
		return errors.New("the file was not saved: the store now holds a file that conflicts with it")
	}
	if err := s.store.Save(ctx, raw); err != nil {
		return fmt.Errorf("save the file: %w", err)
	}
	return nil
}
