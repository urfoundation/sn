//go:build linux || darwin

package validator

// A retained intent history that grows past half its live file limit is
// sealed: its oldest entries move into an immutable, content-addressed
// segment file, and the live steering-intents.json pins every segment, in
// order, by the content hash and size of its exact bytes, its entry count and
// the intent digest chain through its last entry. Every reader assembles the
// same logical history from the segments and the live file, so each rule, the
// verify-once memo and the startup verified record see the sequence an
// unsealed file would hold. Nothing is deleted.
//
// Sealing publishes the segment first, create-only and synced, and the live
// file that pins it second, through the intent store's marker and exchange.
// A crash between the two leaves the previous live file, still holding every
// entry, and an unpinned segment that the next seal of the same entries
// reuses byte for byte.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	steeringIntentSegmentSchema    = "urnetwork-validator-steering-intent-segment-v1"
	steeringIntentSegmentDirectory = "intent-segments"
	// Each file's finite census, as for the unsealed history.
	steeringIntentFileCensus = 16384
	// Sealing keeps at least this many history entries live, the memo's
	// options tail, besides the current intent.
	steeringIntentLiveHistory = releaseIntentV2VerifiedTail
)

// A segment holds history entries exactly as a live file's history does.
type steeringIntentSegmentFile struct {
	Schema  string           `json:"schema"`
	History []SteeringIntent `json:"history"`
}

// The state-relative content-addressed name of a segment.
func steeringIntentSegmentName(contentHash string) string {
	return filepath.ToSlash(filepath.Join(steeringIntentSegmentDirectory, strings.TrimPrefix(contentHash, "sha256:")+".json"))
}

// SteeringIntentSegmentName reports whether name is exactly a sealed
// segment's state-relative content-addressed name.
func SteeringIntentSegmentName(name string) bool {
	hash, found := strings.CutPrefix(name, steeringIntentSegmentDirectory+"/")
	hash, suffixed := strings.CutSuffix(hash, ".json")
	if !found || !suffixed {
		return false
	}
	_, err := parseReleaseContentHash("sha256:" + hash)
	return err == nil && steeringIntentSegmentName("sha256:"+hash) == name
}

// A live intent file is canonical, and either unsealed, with the intent
// schema and no segments, or sealed, with its own schema and at least one.
func decodeSteeringIntentFileV2(ctx context.Context, encoded []byte, limit uint64) (*steeringIntentFile, error) {
	var file steeringIntentFile
	if err := decodeAttemptStreamV2JSON(encoded, limit, &file); err != nil {
		return nil, err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, &file, limit, true, true)
	sealed := len(file.Segments) != 0
	if err != nil || !bytes.Equal(encoded, canonical) || sealed && file.Schema != SteeringIntentSealedSchema || !sealed && file.Schema != steeringIntentSchema ||
		len(file.History) > steeringIntentFileCensus || len(file.Segments) > steeringIntentFileCensus {
		return nil, errors.Join(errors.New("V2 intent history bytes, schema or finite census differ"), err)
	}
	return &file, nil
}

// The complete retained history in logical order: every segment's entries,
// the live history, then the current intent.
type steeringIntentHistoryV2 struct {
	file *steeringIntentFile
	all  []*SteeringIntent
	// Entries before the current intent.
	history int
	// Each segment's exact bytes, in order.
	segments [][]byte
	// The intent digest chain over all, computed only for a sealed file.
	digests [][32]byte
}

// Reads every segment the live file pins through read, which returns the
// bytes at the segment's state-relative name and at most maximum of them. A
// segment whose bytes, schema, canonical form, count or digest chain head
// differs from its pin is refused.
func readSteeringIntentHistoryV2(ctx context.Context, file *steeringIntentFile, limit uint64, read func(name string, maximum uint64) ([]byte, error)) (*steeringIntentHistoryV2, error) {
	if ctx == nil || file == nil || read == nil {
		return nil, errors.New("V2 intent history reader is incomplete")
	}
	result := &steeringIntentHistoryV2{file: file}
	ends := make([]int, 0, len(file.Segments))
	for index, pin := range file.Segments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := parseReleaseContentHash(pin.SHA256); err != nil {
			return nil, err
		}
		if _, err := canonicalAttemptHex32("V2 intent segment head", pin.Head, false); err != nil {
			return nil, err
		}
		if pin.Bytes == 0 || pin.Bytes > limit || pin.Count == 0 || pin.Count > steeringIntentFileCensus {
			return nil, fmt.Errorf("V2 intent segment %d pin is outside its finite bounds", index)
		}
		encoded, err := read(steeringIntentSegmentName(pin.SHA256), pin.Bytes)
		if err != nil {
			return nil, fmt.Errorf("V2 intent segment %d: %w", index, err)
		}
		if uint64(len(encoded)) != pin.Bytes || ReleaseMeasurementContentHash(encoded) != pin.SHA256 {
			return nil, fmt.Errorf("V2 intent segment %d bytes differ from their pin", index)
		}
		segment := &steeringIntentSegmentFile{}
		if err := decodeAttemptStreamV2JSON(encoded, limit, segment); err != nil {
			return nil, err
		}
		canonical, err := marshalAttemptSettlementV2JSON(ctx, segment, limit, true, true)
		if err != nil || !bytes.Equal(encoded, canonical) || segment.Schema != steeringIntentSegmentSchema || uint64(len(segment.History)) != pin.Count {
			return nil, errors.Join(fmt.Errorf("V2 intent segment %d is not its canonical pinned census", index), err)
		}
		for entry := range segment.History {
			result.all = append(result.all, &segment.History[entry])
		}
		ends = append(ends, len(result.all))
		result.segments = append(result.segments, encoded)
	}
	for index := range file.History {
		result.all = append(result.all, &file.History[index])
	}
	result.history = len(result.all)
	if file.Current != nil {
		result.all = append(result.all, file.Current)
	}
	if len(file.Segments) != 0 {
		digests, digestible := releaseIntentV2EntryDigests(result.all)
		if !digestible {
			return nil, errors.New("V2 sealed intent history has no digest chain")
		}
		for index, end := range ends {
			if attemptHex32(digests[end-1]) != file.Segments[index].Head {
				return nil, fmt.Errorf("V2 intent segment %d differs from its pinned position in the history", index)
			}
		}
		result.digests = digests
	}
	return result, nil
}

// SteeringIntentControl reports whether raw has the shape of one retained
// intent control file: a live intent file, sealed or not, that holds an
// intent, or one of its sealed segments. Only the shape is checked: strict
// fields and each file's schema, not canonical bytes or what the file pins.
func SteeringIntentControl(raw []byte) bool {
	limit := uint64(len(raw))
	var file steeringIntentFile
	if decodeAttemptStreamV2JSON(raw, limit, &file) == nil {
		sealed := len(file.Segments) != 0
		return (sealed && file.Schema == SteeringIntentSealedSchema || !sealed && file.Schema == steeringIntentSchema) && (file.Current != nil || len(file.History) != 0 || sealed)
	}
	var segment steeringIntentSegmentFile
	return decodeAttemptStreamV2JSON(raw, limit, &segment) == nil && segment.Schema == steeringIntentSegmentSchema && len(segment.History) != 0
}

// ReadSteeringIntentHistory decodes a validator's live steering-intents.json
// and every segment it pins, through read as for the validator's own readers,
// and returns the retained intents in order with each one's exact stored
// bytes. history counts the entries before the current intent.
func ReadSteeringIntentHistory(ctx context.Context, encoded []byte, limit uint64, read func(name string, maximum uint64) ([]byte, error)) (intents []SteeringIntent, raw []json.RawMessage, history int, resultErr error) {
	file, err := decodeSteeringIntentFileV2(ctx, encoded, limit)
	if err != nil {
		return nil, nil, 0, err
	}
	logical, err := readSteeringIntentHistoryV2(ctx, file, limit, read)
	if err != nil {
		return nil, nil, 0, err
	}
	var live struct {
		Schema   string                  `json:"schema"`
		Segments []SteeringIntentSegment `json:"segments,omitempty"`
		Current  json.RawMessage         `json:"current,omitempty"`
		History  []json.RawMessage       `json:"history"`
	}
	for _, segment := range logical.segments {
		var wire struct {
			Schema  string            `json:"schema"`
			History []json.RawMessage `json:"history"`
		}
		if err := json.Unmarshal(segment, &wire); err != nil {
			return nil, nil, 0, err
		}
		raw = append(raw, wire.History...)
	}
	if err := json.Unmarshal(encoded, &live); err != nil {
		return nil, nil, 0, err
	}
	raw = append(raw, live.History...)
	if file.Current != nil {
		raw = append(raw, live.Current)
	}
	if len(raw) != len(logical.all) {
		return nil, nil, 0, errors.New("V2 intent history raw census differs")
	}
	intents = make([]SteeringIntent, len(logical.all))
	for index, intent := range logical.all {
		intents[index] = *intent
	}
	return intents, raw, logical.history, nil
}

// Moves all but the last live history entries into one new segment when the
// live file would pass half its limit. The sealed entries are all already
// published, so a retry after a crash seals the same bytes. The caller
// publishes the live file that pins the segment.
func (self *IntentStore) sealHistoryV2(ctx context.Context, read *releaseIntentV2Read, limit uint64) error {
	sealed := len(read.file.History) - steeringIntentLiveHistory
	if sealed <= 0 {
		return nil
	}
	size, err := countAttemptSettlementV2JSON(ctx, read.file, limit, true, true)
	if err == nil && size <= limit/2 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	earlier := 0
	for _, pin := range read.file.Segments {
		earlier += int(pin.Count)
	}
	if earlier+sealed > len(read.digests) {
		return errors.New("V2 intent seal lacks the digest chain of its published history")
	}
	segment := steeringIntentSegmentFile{Schema: steeringIntentSegmentSchema, History: slices.Clone(read.file.History[:sealed])}
	encoded, err := marshalAttemptSettlementV2JSON(ctx, &segment, limit, true, true)
	if err != nil {
		return err
	}
	pin := SteeringIntentSegment{SHA256: ReleaseMeasurementContentHash(encoded), Bytes: uint64(len(encoded)), Count: uint64(sealed), Head: attemptHex32(read.digests[earlier+sealed-1])}
	path := filepath.Join(self.stateDir, filepath.FromSlash(steeringIntentSegmentName(pin.SHA256)))
	if err := persistSteeringIntentSegment(ctx, path, encoded); err != nil {
		return err
	}
	// Newly referenced bytes have the same custody obligation as every older
	// segment. Keep their native owner through the live-file exchange.
	owned, err := read.custody.read(ctx, path, pin.Bytes, false)
	if err != nil || !bytes.Equal(owned, encoded) {
		return errors.Join(errors.New("V2 newly sealed intent segment changed before custody"), err)
	}
	read.file.Schema = SteeringIntentSealedSchema
	read.file.Segments = append(slices.Clone(read.file.Segments), pin)
	read.file.History = slices.Clone(read.file.History[sealed:])
	return ctx.Err()
}

// Publishes a segment create-only: a complete synced temporary is linked to
// the content-addressed name, so that name never holds a partial file. An
// existing name must already hold exactly these bytes in this account's
// private regular file, as after a crash before a live file pinned it.
func persistSteeringIntentSegment(ctx context.Context, path string, encoded []byte) (resultErr error) {
	// Share the existing descriptor-owned immutable publisher: all parent
	// creation, no-replace linking, cleanup and durability use retained owners.
	owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, uint64(len(encoded)), releaseMeasurementInputV2ReadHooks{}, true)
	defer func() { resultErr = errors.Join(resultErr, owner.finish()) }()
	if err != nil {
		return err
	}
	if owner.directory.anchor.mode&0o077 != 0 || owner.directory.anchor.uid != uint32(os.Geteuid()) {
		return errors.New("V2 intent segment directory is not private and owned; existing directories are not repaired")
	}
	_, err = owner.read()
	if err != nil && !(owner.initialMissing && releaseMeasurementInputV2OnlyMissing(err)) {
		return err
	}
	return owner.write(encoded)
}
