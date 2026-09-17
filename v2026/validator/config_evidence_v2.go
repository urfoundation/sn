package validator

// Runtime configuration names finite owners and exact external inputs. These
// references are not authenticated activation, migration or history verdicts.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/protocol"
)

const ReleaseEvidenceV2ConfigSchema = "urnetwork-validator-runtime-evidence-v2"

// The existing runtime bounds remain the source of truth, including their
// native YAML field names. No constructor default is installed by this layer.
type ReleaseEvidenceV2Bounds struct {
	Disk                 AttemptLedgerDiskLimits                     `yaml:"disk" json:"disk"`
	Cut                  AttemptCutV2Bounds                          `yaml:"cut" json:"cut"`
	Replay               AttemptCutV2ReplayBounds                    `yaml:"replay" json:"replay"`
	Persistence          AttemptSettlementRuntimeV2PersistenceBounds `yaml:"persistence" json:"persistence"`
	HeadEMA              HeadEMAStoreV2Limits                        `yaml:"head_ema" json:"head_ema"`
	MaxProviders         uint64                                      `yaml:"max_providers" json:"max_providers"`
	MaxEgressHashes      uint64                                      `yaml:"max_egress_hashes" json:"max_egress_hashes"`
	MaxFleetPrefixes     uint64                                      `yaml:"max_fleet_prefixes" json:"max_fleet_prefixes"`
	MaxOperators         uint64                                      `yaml:"max_operators" json:"max_operators"`
	MaxHeadEntries       uint64                                      `yaml:"max_head_entries" json:"max_head_entries"`
	MaxArtifactBytes     uint64                                      `yaml:"max_artifact_bytes" json:"max_artifact_bytes"`
	MaxControlBytes      uint64                                      `yaml:"max_control_bytes" json:"max_control_bytes"`
	MaxInputJournalBytes uint64                                      `yaml:"max_input_journal_bytes" json:"max_input_journal_bytes"`
	MaxParticipants      uint64                                      `yaml:"max_participants" json:"max_participants"`
	MaxTransitionBytes   uint64                                      `yaml:"max_transition_bytes" json:"max_transition_bytes"`
	MaxClosureBytes      uint64                                      `yaml:"max_closure_bytes" json:"max_closure_bytes"`
	MaxHistoryBytes      uint64                                      `yaml:"max_history_bytes" json:"max_history_bytes"`
	MaxCaptureFiles      uint64                                      `yaml:"max_capture_files,omitempty" json:"max_capture_files,omitempty"`
}

// Old configurations retain their original count ceiling. A larger capture
// census requires an explicit deployment bound, independent of byte capacity.
const ReleaseEvidenceV2DefaultCaptureFiles = uint64(16384)

// A zero optional field preserves the prior admission; it never grants a
// topology-derived increase to a runtime or an unqualified direct caller.
func (self ReleaseEvidenceV2Bounds) CaptureFileLimit() uint64 {
	if self.MaxCaptureFiles == 0 {
		return ReleaseEvidenceV2DefaultCaptureFiles
	}
	return self.MaxCaptureFiles
}

// The intent store is one control document within the aggregate history. Its
// own maximum cannot consume that entire history and all its references too.
func (self ReleaseEvidenceV2Bounds) IntentFileLimit() uint64 {
	return min(self.MaxControlBytes, self.MaxHistoryBytes)
}

// A reference pins exact bytes, not merely a mutable filename. History remains
// storage-choice-independent until its separate public authority reader exists.
type ReleaseEvidenceV2File struct {
	Path   string `yaml:"path" json:"path"`
	Bytes  uint64 `yaml:"bytes" json:"bytes"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

// Scratch roots own fresh per-operation children; a configured root itself
// must never be reused as the replay/seal API's must-not-exist scratch path.
type ReleaseEvidenceV2OperatorConfig struct {
	NoID              uint64                `yaml:"no_id" json:"no_id"`
	Activation        ReleaseEvidenceV2File `yaml:"activation" json:"activation"`
	VPKSignature      ReleaseEvidenceV2File `yaml:"vpk_signature" json:"vpk_signature"`
	HotkeySignature   ReleaseEvidenceV2File `yaml:"hotkey_signature" json:"hotkey_signature"`
	Context           ReleaseEvidenceV2File `yaml:"context" json:"context"`
	History           ReleaseEvidenceV2File `yaml:"history" json:"history"`
	ReplayScratchRoot string                `yaml:"replay_scratch_root" json:"replay_scratch_root"`
	SealScratchRoot   string                `yaml:"seal_scratch_root" json:"seal_scratch_root"`
}

// The complete operator census is explicit, including on restarts. A partial
// list cannot turn missing configured members into an accepted subset.
type ReleaseEvidenceV2Config struct {
	Schema              string                            `yaml:"schema" json:"schema"`
	UploadIntentSeconds uint64                            `yaml:"upload_intent_seconds,omitempty" json:"upload_intent_seconds,omitempty"`
	Bounds              ReleaseEvidenceV2Bounds           `yaml:"bounds" json:"bounds"`
	Operators           []ReleaseEvidenceV2OperatorConfig `yaml:"operators" json:"operators"`
}

// Simulator input uses the same value it renders, without reducing integers
// through interface{} / float64 or substituting a topology-derived allowance.
type ReleaseValidatorEvidenceV2Config struct {
	ValidatorID uint64                  `yaml:"validator_id" json:"validator_id"`
	Evidence    ReleaseEvidenceV2Config `yaml:"evidence" json:"evidence"`
}

// YAML's permissive numeric conversions are not capacity policy. Reject
// aliases, merges, duplicates, unknown keys, nulls and non-decimal integers
// before the typed decoder runs; the shape is this fixed local schema only.
func validateReleaseEvidenceV2YAML(node *yaml.Node, valueType reflect.Type) error {
	if node == nil || node.Kind == yaml.AliasNode || node.Alias != nil {
		return errors.New("evidence_v2 YAML aliases are forbidden")
	}
	switch valueType.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode || node.Tag != "!!map" || len(node.Content)%2 != 0 {
			return errors.New("evidence_v2 YAML requires an object")
		}
		seen := map[string]bool{}
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
				return errors.New("evidence_v2 YAML has a duplicate or non-string key")
			}
			seen[key.Value] = true
			fieldIndex := -1
			for candidate := 0; candidate < valueType.NumField(); candidate++ {
				field := valueType.Field(candidate)
				name := strings.SplitN(field.Tag.Get("yaml"), ",", 2)[0]
				if name == "" {
					name = strings.ToLower(field.Name)
				}
				if name == key.Value {
					fieldIndex = candidate
					break
				}
			}
			if fieldIndex < 0 {
				return fmt.Errorf("evidence_v2 YAML has unknown field %q", key.Value)
			}
			if err := validateReleaseEvidenceV2YAML(node.Content[index+1], valueType.Field(fieldIndex).Type); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if node.Kind != yaml.SequenceNode || node.Tag != "!!seq" {
			return errors.New("evidence_v2 YAML requires a sequence")
		}
		for _, child := range node.Content {
			if err := validateReleaseEvidenceV2YAML(child, valueType.Elem()); err != nil {
				return err
			}
		}
	case reflect.Uint64:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			return errors.New("evidence_v2 requires unquoted decimal uint64 values")
		}
		parsed, err := strconv.ParseUint(node.Value, 10, 64)
		if err != nil || strconv.FormatUint(parsed, 10) != node.Value {
			return errors.New("evidence_v2 integer is non-canonical or overflows uint64")
		}
	case reflect.String:
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" || !utf8.ValidString(node.Value) {
			return errors.New("evidence_v2 requires a UTF-8 string")
		}
	default:
		return errors.New("evidence_v2 configuration schema has an unsupported type")
	}
	return nil
}

// Decode only after the independent schema admission above.
func (self *ReleaseEvidenceV2Config) UnmarshalYAML(node *yaml.Node) error {
	if err := validateReleaseEvidenceV2YAML(node, reflect.TypeFor[ReleaseEvidenceV2Config]()); err != nil {
		return err
	}
	type plain ReleaseEvidenceV2Config
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*self = ReleaseEvidenceV2Config(decoded)
	return nil
}

// The simulator's routing key has the same strict numeric grammar.
func (self *ReleaseValidatorEvidenceV2Config) UnmarshalYAML(node *yaml.Node) error {
	if err := validateReleaseEvidenceV2YAML(node, reflect.TypeFor[ReleaseValidatorEvidenceV2Config]()); err != nil {
		return err
	}
	type plain ReleaseValidatorEvidenceV2Config
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*self = ReleaseValidatorEvidenceV2Config(decoded)
	return nil
}

// YAML resolves field aliases and bypasses custom decoders for null before
// UnmarshalYAML is called. Inspect the original node at each file boundary.
func validateReleaseEvidenceV2Document(wire []byte, field string, valueType reflect.Type) error {
	var document yaml.Node
	if err := yaml.Unmarshal(wire, &document); err != nil {
		return err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	root := document.Content[0]
	for index := 0; index+1 < len(root.Content); index += 2 {
		if root.Content[index].Value == field {
			if err := validateReleaseEvidenceV2YAML(root.Content[index+1], valueType); err != nil {
				return err
			}
		}
	}
	return nil
}

// This structural admission complements the loader's full KnownFields and
// single-document checks; it never replaces the complete config validation.
func ValidateReleaseEvidenceV2ConfigYAML(wire []byte) error {
	return validateReleaseEvidenceV2Document(wire, "evidence_v2", reflect.TypeFor[ReleaseEvidenceV2Config]())
}

// Planning may omit the list; if supplied, every original node is strict.
func ValidateReleaseValidatorEvidenceV2YAML(wire []byte) error {
	return validateReleaseEvidenceV2Document(wire, "validator_evidence_v2", reflect.TypeFor[[]ReleaseValidatorEvidenceV2Config]())
}

// All configured dimensions must survive signed lengths and size+1 reads.
// These are necessary admissions, not maximum-wire or all-pair capacity proof.
func (self ReleaseEvidenceV2Bounds) Validate(operators uint64) error {
	self.MaxCaptureFiles = self.CaptureFileLimit()
	var finite func(reflect.Value) bool
	finite = func(value reflect.Value) bool {
		if value.Kind() == reflect.Struct {
			for index := 0; index < value.NumField(); index++ {
				if !finite(value.Field(index)) {
					return false
				}
			}
			return true
		}
		return value.Kind() == reflect.Uint64 && value.Uint() > 0 && value.Uint() < uint64(^uint(0)>>1)
	}
	if !finite(reflect.ValueOf(self)) {
		return errors.New("evidence_v2 bounds must all be explicit, nonzero and fit signed size+1")
	}
	if operators == 0 || operators > self.MaxOperators || operators > self.MaxParticipants {
		return errors.New("evidence_v2 configured operator census exceeds its bounds")
	}
	if err := errors.Join(self.Cut.Records.Validate(), self.Cut.Proofs.Validate(), self.Replay.validate(self.Cut), self.Persistence.validate(), self.HeadEMA.validate()); err != nil {
		return err
	}
	// The real replicated sealer publishes its signed header through this same
	// typed public metadata endpoint. Reject an unusable advertised allowance
	// before bootstrap files, RPC or disk initialization can begin.
	metadataBytes := max(self.Cut.Records.MaxManifestBytes, self.Cut.Records.MaxPageBytes, self.Cut.Proofs.MaxManifestBytes, self.Cut.Proofs.MaxPageBytes)
	if self.Cut.MaxHeaderBytes > metadataBytes {
		return errors.New("evidence_v2 header allowance exceeds its public metadata bound")
	}
	if err := validateReleaseMeasurementInputV2Limit(self.MaxInputJournalBytes); err != nil {
		return err
	}
	disk := self.Disk
	if disk.MaxRecordBytes > uint64(^uint(0)>>1)/8 || disk.MaxTrailCount > disk.MaxRecordCount || disk.MaxRawRecordBytes < disk.MaxRecordBytes || disk.MaxStorageBytes <= attemptStoreMetadataReserve || disk.MaxStorageFiles < 8 {
		return errors.New("evidence_v2 disk limits are inconsistent")
	}
	if self.MaxArtifactBytes > maxReleaseMeasurementArtifactBytes || self.MaxControlBytes > maxReleaseMeasurementArtifactBytes || self.MaxTransitionBytes > self.MaxClosureBytes || self.MaxClosureBytes > self.MaxArtifactBytes {
		return errors.New("evidence_v2 metadata allowances conflict with the retained envelope bound")
	}
	if disk.MaxRecordBytes > self.Replay.MaxRecordBytes || self.Replay.MaxTrails < disk.MaxTrailCount || self.Cut.Records.MaxItems < disk.MaxRecordCount || self.Cut.Proofs.MaxItems < disk.MaxTrailCount || self.Cut.Records.MaxDataBytes < disk.MaxRawRecordBytes || self.Cut.Proofs.MaxDataBytes < disk.MaxProofBytes {
		return errors.New("evidence_v2 streams or replay cannot contain the configured disk allowance")
	}
	for _, stream := range []struct {
		kind   string
		bounds AttemptStreamV2Bounds
		row    uint64
	}{
		{kind: AttemptStreamV2Records, bounds: self.Cut.Records, row: self.Replay.MaxRecordBytes},
		{kind: AttemptStreamV2Proofs, bounds: self.Cut.Proofs, row: self.Replay.MaxProofBytes},
	} {
		if _, err := attemptStreamV2WritePageCapacity(stream.kind, AttemptStreamV2WriteOptions{Bounds: stream.bounds, MaxRowBytes: stream.row}); err != nil {
			return err
		}
	}
	// Bound the sealer's two descriptor spools before its multiplication.
	maximum := uint64(^uint(0)>>1) - 1
	if self.Cut.Records.MaxChunks > maximum/attemptStreamV2DescriptorBytes || self.Cut.Proofs.MaxChunks > maximum/attemptStreamV2DescriptorBytes-self.Cut.Records.MaxChunks {
		return errors.New("evidence_v2 aggregate descriptor spool bound overflows")
	}
	spoolBytes := (self.Cut.Records.MaxChunks + self.Cut.Proofs.MaxChunks) * attemptStreamV2DescriptorBytes
	if self.Replay.MaxScratchBytes > maximum-spoolBytes {
		return errors.New("evidence_v2 aggregate sealing scratch bound overflows")
	}
	return nil
}

// This spelling check precedes legacy normalization; evidence authority is
// never silently cleaned, made absolute or redirected through an ancestor link.
func ValidateReleaseEvidenceV2Path(path string) error {
	if !utf8.ValidString(path) || strings.ContainsRune(path, 0) || path != strings.TrimSpace(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return errors.New("evidence_v2 path must be canonical absolute non-root UTF-8")
	}
	current := string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || index+1 < len(parts) && !info.IsDir() {
			return errors.New("evidence_v2 path has a symlink or non-directory ancestor")
		}
	}
	return nil
}

// Missing directories may be provisioned by the later root owner. Existing
// directories must already be private; configuration never repairs modes.
func validateReleaseEvidenceV2Directory(path string) error {
	if err := ValidateReleaseEvidenceV2Path(path); err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	directory, err := openAttemptPrivateDirectory(path)
	if err != nil {
		return err
	}
	if directory.anchor.uid != uint32(os.Geteuid()) || directory.anchor.mode&0o7777 != 0o700 {
		return errors.Join(errors.New("evidence_v2 directory is not owner-private"), directory.close())
	}
	return errors.Join(directory.check(), directory.close())
}

// Returns the complete fixed role inventory, not candidate-discovered paths.
func (self ReleaseEvidenceV2OperatorConfig) Files() []ReleaseEvidenceV2File {
	return []ReleaseEvidenceV2File{self.Activation, self.VPKSignature, self.HotkeySignature, self.Context, self.History}
}

// This is a content reference check only; callers still authenticate decoded
// activation, both signatures, historical eligibility and inclusion/finality.
func (self ReleaseEvidenceV2File) Validate(maxBytes uint64) error {
	if self.Bytes == 0 || self.Bytes > maxBytes || self.Bytes >= uint64(^uint(0)>>1) {
		return errors.New("evidence_v2 reference length is missing or exceeds its independent bound")
	}
	if len(self.SHA256) != 66 || !strings.HasPrefix(self.SHA256, "0x") {
		return errors.New("evidence_v2 reference hash is missing or non-canonical")
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(self.SHA256, "0x"))
	if err != nil || len(digest) != sha256.Size || self.SHA256 != "0x"+hex.EncodeToString(digest) || self.SHA256 == "0x"+strings.Repeat("0", 64) {
		return errors.New("evidence_v2 reference hash is missing or non-canonical")
	}
	return ValidateReleaseEvidenceV2Path(self.Path)
}

// Canonical sorting is part of rendered/runtime identity. Durable operator
// roots may descend from the coordinator, but scratch and reference inputs may
// not overlap any durable root, credential, each other or another operator.
func (self ReleaseEvidenceV2Config) Validate(operators []OperatorConfig, coordinator, hotkey string) error {
	if self.Schema != ReleaseEvidenceV2ConfigSchema {
		return errors.New("production requires explicit evidence_v2 configuration")
	}
	// Historical planning inputs may omit staging. Runtime source ownership
	// separately requires an explicit positive lifetime before any upload.
	if self.UploadIntentSeconds > 3600 {
		return errors.New("evidence_v2 upload intent lifetime exceeds one hour")
	}
	if err := self.Bounds.Validate(uint64(len(operators))); err != nil {
		return err
	}
	if len(self.Operators) != len(operators) {
		return errors.New("evidence_v2 operator census differs from configured operators")
	}
	if err := validateReleaseEvidenceV2Directory(coordinator); err != nil {
		return err
	}
	if err := ValidateReleaseEvidenceV2Path(hotkey); err != nil {
		return err
	}
	overlaps := func(first, second string) bool {
		return first == second || strings.HasPrefix(first, second+string(filepath.Separator)) || strings.HasPrefix(second, first+string(filepath.Separator))
	}
	protected := []string{coordinator, hotkey}
	byNO := map[uint64]OperatorConfig{}
	for _, operator := range operators {
		if operator.NoID == 0 || byNO[operator.NoID].NoID != 0 {
			return errors.New("evidence_v2 operator identity is zero or duplicated")
		}
		if err := validateReleaseEvidenceV2Directory(operator.StateDir); err != nil {
			return err
		}
		if operator.StateDir == coordinator || strings.HasPrefix(coordinator, operator.StateDir+string(filepath.Separator)) {
			return errors.New("evidence_v2 operator cannot contain coordinator state")
		}
		if overlaps(operator.StateDir, hotkey) {
			return errors.New("evidence_v2 operator state overlaps the validator hotkey")
		}
		for _, prior := range byNO {
			if overlaps(operator.StateDir, prior.StateDir) {
				return errors.New("evidence_v2 operator state namespaces overlap")
			}
		}
		byNO[operator.NoID] = operator
		protected = append(protected, operator.StateDir)
		for _, path := range []string{operator.NetworkJWTFile, operator.ClientJWTFile, operator.ClientKeySeedFile} {
			if path != "" {
				if err := ValidateReleaseEvidenceV2Path(path); err != nil {
					return err
				}
				protected = append(protected, path)
			}
		}
	}
	for noID, operator := range byNO {
		for _, path := range []string{operator.NetworkJWTFile, operator.ClientJWTFile, operator.ClientKeySeedFile} {
			if path == "" {
				continue
			}
			if overlaps(path, hotkey) {
				return errors.New("evidence_v2 operator credential overlaps the validator hotkey")
			}
			for otherID, other := range byNO {
				if otherID != noID && overlaps(path, other.StateDir) {
					return errors.New("evidence_v2 credential crosses operator state ownership")
				}
			}
		}
	}
	owned := []string{}
	var priorNO uint64
	for _, operator := range self.Operators {
		if operator.NoID <= priorNO || byNO[operator.NoID].NoID == 0 {
			return errors.New("evidence_v2 operator census must be exact, unique and sorted")
		}
		priorNO = operator.NoID
		if operator.Activation.Bytes != uint64(protocol.ValidatorEvidenceActivationPayloadSize) || operator.VPKSignature.Bytes != 64 || operator.HotkeySignature.Bytes != 64 {
			return errors.New("evidence_v2 activation or signature reference has the wrong exact width")
		}
		limits := []uint64{uint64(protocol.ValidatorEvidenceActivationPayloadSize), 64, 64, self.Bounds.Cut.MaxHeaderBytes, self.Bounds.MaxHistoryBytes}
		for index, file := range operator.Files() {
			if err := file.Validate(limits[index]); err != nil {
				return err
			}
			owned = append(owned, file.Path)
		}
		owned = append(owned, operator.ReplayScratchRoot, operator.SealScratchRoot)
		for _, path := range []string{operator.ReplayScratchRoot, operator.SealScratchRoot} {
			if err := validateReleaseEvidenceV2Directory(path); err != nil {
				return err
			}
		}
	}
	for index, path := range owned {
		if err := ValidateReleaseEvidenceV2Path(path); err != nil {
			return err
		}
		for _, other := range protected {
			if overlaps(path, other) {
				return errors.New("evidence_v2 reference or scratch namespace overlaps protected state or another owner")
			}
		}
		for _, other := range owned[:index] {
			if overlaps(path, other) {
				return errors.New("evidence_v2 reference or scratch namespace overlaps protected state or another owner")
			}
		}
	}
	return nil
}

// Bounded read-only content checking serves rendering and later authority
// readers. It does not decode or endorse a proof, nor create/chmod any path.
func ReadReleaseEvidenceV2File(ctx context.Context, reference ReleaseEvidenceV2File, maxBytes uint64) (result []byte, resultErr error) {
	return readReleaseEvidenceV2File(ctx, reference, maxBytes, nil, nil)
}

// Call-local observation barriers preserve the real open/read/close path.
func readReleaseEvidenceV2File(ctx context.Context, reference ReleaseEvidenceV2File, maxBytes uint64, afterStat, afterRead func()) (result []byte, resultErr error) {
	if ctx == nil {
		return nil, errors.New("evidence_v2 reference context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := reference.Validate(maxBytes); err != nil {
		return nil, err
	}
	directory, err := openAttemptPrivateDirectory(filepath.Dir(reference.Path))
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, directory.check(), directory.close(), ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if directory.anchor.uid != uint32(os.Geteuid()) || directory.anchor.mode&0o7777 != 0o700 {
		return nil, errors.New("evidence_v2 reference directory is not owner-private")
	}
	name := filepath.Base(reference.Path)
	before, err := directory.stat(name)
	if err != nil {
		return nil, err
	}
	if !before.regular() || before.mode&0o7777 != 0o600 || before.uid != uint32(os.Geteuid()) || before.links != 1 || before.size < 0 || uint64(before.size) != reference.Bytes {
		return nil, errors.New("evidence_v2 reference is not an exact private regular file")
	}
	if afterStat != nil {
		afterStat()
	}
	file, err := directory.openFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		afterDescriptor, statErr := statAttemptPrivateFile(file)
		resultErr = errors.Join(resultErr, statErr, file.Close(), ctx.Err())
		after, err := directory.stat(name)
		if err != nil || before != after || before != afterDescriptor {
			resultErr = errors.Join(resultErr, errors.New("evidence_v2 reference changed during read"), err)
		}
		if resultErr != nil {
			result = nil
		}
	}()
	opened, err := statAttemptPrivateFile(file)
	if err != nil || before != opened {
		return nil, errors.Join(errors.New("evidence_v2 reference descriptor differs"), err)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(reference.Bytes)+1))
	if err != nil {
		return nil, err
	}
	if afterRead != nil {
		afterRead()
	}
	digest := sha256.Sum256(data)
	if uint64(len(data)) != reference.Bytes || reference.SHA256 != "0x"+hex.EncodeToString(digest[:]) {
		return nil, errors.New("evidence_v2 reference bytes differ from their configured identity")
	}
	return data, nil
}
