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
	"time"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"
)

const maxFileBytes = 256 << 10

// signatureText is a signature as the file holds it: 0x and 65 bytes of lowercase hex.
var signatureText = regexp.MustCompile(`^0x[0-9a-f]{130}$`)

// File is a parsed amendments file: its signed entries in file order.
type File struct {
	Netuid  uint16
	Entries []Entry
	// verified is set by Verify once every entry has checked out. At answers only
	// then, so no reader can apply an entry nobody verified.
	verified bool
}

// Unpublished is the file of a deployment with no pinned signer, which publishes no
// amendments: none is signed, so only its configuration can put one in force.
func Unpublished(netuid uint16) *File {
	return &File{Netuid: netuid, verified: true}
}

// Verify checks that the file is for this subnet and that every entry is for this
// deployment and signed by signer.
func (f *File) Verify(chainID uint64, netuid uint16, signer common.Address) error {
	if f == nil {
		return errors.New("amendment: no file")
	}
	f.verified = false
	if f.Netuid != netuid {
		return fmt.Errorf("amendment: the file is for sn %d, not sn %d", f.Netuid, netuid)
	}
	for i := range f.Entries {
		if err := f.Entries[i].Verify(chainID, netuid, signer); err != nil {
			return err
		}
	}
	f.verified = true
	return nil
}

// Latest is the entry of kind for operator noID that takes effect last, nil when the
// file has none.
func (f *File) Latest(kind Kind, noID uint64) *Entry {
	if f == nil {
		return nil
	}
	var latest *Entry
	for i := range f.Entries {
		if f.Entries[i].Kind() == kind && f.Entries[i].NoID == noID {
			latest = &f.Entries[i]
		}
	}
	return latest
}

// InForce is the amendments in force for one network operator in one epoch, nil for
// a kind with none in force.
type InForce struct {
	// Extenders is the extender pool's entry; its FromEpoch is E_x.
	Extenders *Entry
	// CaptureTheFlag is capture the flag's entry; its FromEpoch is E_c.
	CaptureTheFlag *Entry
}

// ErrUnknown is At's answer without a verified file. What is in force is then
// unknown, and configuration does not stand in for it: a signed entry the reader
// cannot see may be in force.
var ErrUnknown = errors.New("amendment: no verified amendments file is held, so the amendments in force are unknown")

// At is the amendments in force for operator noID in epoch: for each kind, the
// signed entry with the latest first epoch at or before it. Only for a kind with no
// signed entry in force does fallback, the deployment's configuration, stand in, as
// WHITEPAPER.md §8.6.7 keeps configuration only for an amendment whose signing
// cannot happen before its first epoch. fallback may be nil.
//
// The file must be verified: a Source returns only verified files, and a file from
// ParseFile is verified by Verify. A nil file is ErrUnknown.
func (f *File) At(noID, epoch uint64, fallback *Fallback) (InForce, error) {
	if f == nil {
		return InForce{}, ErrUnknown
	}
	if !f.verified {
		return InForce{}, errors.New("amendment: the file has not been verified")
	}
	var configured []Entry
	if fallback != nil {
		configured = fallback.entries
	}
	at := func(kind Kind) *Entry {
		if entry := inForce(f.Entries, kind, noID, epoch); entry != nil {
			return entry
		}
		return inForce(configured, kind, noID, epoch)
	}
	return InForce{Extenders: at(KindExtenders), CaptureTheFlag: at(KindCaptureTheFlag)}, nil
}

// inForce is the entry of kind for operator noID with the latest first epoch at or
// before epoch, copied, nil for none. Entries of a kind for an operator take effect in
// the order they are listed (checkOrder), so the last that has taken effect is it.
func inForce(entries []Entry, kind Kind, noID, epoch uint64) *Entry {
	var found *Entry
	for i := range entries {
		if entries[i].Kind() == kind && entries[i].NoID == noID && entries[i].FromEpoch <= epoch {
			found = &entries[i]
		}
	}
	if found == nil {
		return nil
	}
	return found.clone()
}

// checkOrder requires the entries of one kind for one operator to take effect in
// increasing order of first epoch, in the order listed. Entries of different kinds or
// operators may interleave.
func checkOrder(entries []Entry) error {
	type series struct {
		kind Kind
		noID uint64
	}
	latest := map[series]uint64{}
	for i := range entries {
		key := series{entries[i].Kind(), entries[i].NoID}
		if previous, ok := latest[key]; ok && entries[i].FromEpoch <= previous {
			return fmt.Errorf("%s is not after the entry from epoch %d before it: entries of one kind for one operator take effect in the order they are listed", &entries[i], previous)
		}
		latest[key] = entries[i].FromEpoch
	}
	return nil
}

// Fallback is a deployment's configured amendments: unsigned lines its configuration
// states, the server's in sn.yml and the validator's in its adopted config. They stand
// in only for a kind with no signed entry in force (File.At): configuration is the
// fallback for an amendment whose signing cannot happen before its first epoch starts
// (WHITEPAPER.md §8.6.7), never for a file that cannot be read.
type Fallback struct {
	entries []Entry
}

// ParseFallback reads a deployment's configured lines. Each must be a valid line for
// this deployment, and those of one kind for one operator must take effect in the
// order listed. No lines is no fallback.
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
		fallback.entries = append(fallback.entries, *entry)
	}
	if err := checkOrder(fallback.entries); err != nil {
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
// value in quotes, on one line and with no escape. A line in the file then reads
// exactly as the line that was signed.
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
		if signatureNode.Kind != yaml.ScalarNode || signatureNode.Style != yaml.DoubleQuotedStyle || !signatureText.MatchString(signatureNode.Value) {
			return nil, fmt.Errorf("amendment: line %d: a signature is 0x and 65 bytes of lowercase hex, in double quotes", signatureNode.Line)
		}
		if entry.Signature, err = hex.DecodeString(signatureNode.Value[2:]); err != nil {
			return nil, fmt.Errorf("amendment: line %d: %w", signatureNode.Line, err)
		}
		entries = append(entries, *entry)
	}
	if err := checkOrder(entries); err != nil {
		return nil, fmt.Errorf("amendment: %w", err)
	}
	return entries, nil
}

var netuidText = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)

// ParseFile reads a file strictly: one YAML document naming its subnet, and every
// entry a valid line for that subnet, written exactly as signed, and a signature.
// Entries of one kind for one operator must take effect in the order listed.
// Top-level fields it does not use are passed over, as the price sheet's reader
// passes them over. It does not check signatures; Verify does.
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
			file.Netuid = uint16(netuid)
		case "signed_amendments":
			if file.Entries, err = parseEntries(raw, pair[1]); err != nil {
				return nil, err
			}
		}
	}
	if file.Netuid == 0 {
		return nil, errors.New("amendment: the file names no sn")
	}
	for i := range file.Entries {
		if file.Entries[i].Netuid != file.Netuid {
			return nil, fmt.Errorf("amendment: %s is for sn %d, in a file for sn %d", &file.Entries[i], file.Entries[i].Netuid, file.Netuid)
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
// changes one is a rollback, not news. ParseFile requires the entries of a kind for
// an operator to take effect in the order listed, so an entry added after those a
// reader holds takes effect after every one of its kind and operator it holds.
func Extends(next, last *File) bool {
	if last == nil {
		return true
	}
	if next == nil || len(next.Entries) < len(last.Entries) {
		return false
	}
	for i := range last.Entries {
		if !sameEntry(&last.Entries[i], &next.Entries[i]) {
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

// Source fetches the published file and holds the last good copy, as the price
// sheet's source does, so an outage or a tampered file changes nothing: an unverified
// file is never used, and the entries in force stay what was last verified.
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
	file, err := ParseFile(raw)
	if err != nil {
		return nil, err, true
	}
	if err := file.Verify(s.chainID, s.netuid, s.signer); err != nil {
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

// Refresh returns the file to use now, verified. problem says what went wrong, for
// an alert: why the published file was not taken, in which case the file returned is
// the last good one, or that the stored copy could not be used. A nil file with a
// problem means no verified file is held and none could be fetched, so the
// amendments in force are unknown (ErrUnknown).
//
// A stored copy that is unreadable or corrupt does not stop the fetch: the published
// file, once verified, is taken as if nothing were held, as after a lost store.
func (s *Source) Refresh(ctx context.Context) (file *File, problem error) {
	last, stored, mayReplace := s.lastGood(ctx)
	report := func(err error) error {
		switch {
		case stored == nil:
			return fmt.Errorf("amendment: %w", err)
		case err == nil:
			return fmt.Errorf("amendment: the stored file: %w", stored)
		default:
			return fmt.Errorf("amendment: %w; the stored file: %v", err, stored)
		}
	}
	raw, err := s.fetch(ctx)
	if err != nil {
		return last, report(err)
	}
	next, err := ParseFile(raw)
	if err != nil {
		return last, report(err)
	}
	if err := next.Verify(s.chainID, s.netuid, s.signer); err != nil {
		return last, report(err)
	}
	if !Extends(next, last) {
		return last, report(errors.New("the published file drops or changes a signed entry, or adds one before it"))
	}
	if mayReplace {
		if err := s.store.Save(ctx, raw); err != nil {
			return next, report(fmt.Errorf("save the file: %w", err))
		}
	}
	if stored != nil {
		return next, report(nil)
	}
	return next, nil
}
