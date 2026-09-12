//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/urfoundation/sn/protocol"
)

// OpenReleaseEvidenceV2Archive reconstructs complete signed history in fresh
// bounded scratch. Original configuration/file pins supply activation and
// initial EMA authority; no candidate cut can choose its own initial cursor.
// This returns mathematical history, not a historical chain observation.
func OpenReleaseEvidenceV2Archive(ctx context.Context, options ReleaseEvidenceV2ArchiveOptions) (result *ReleaseEvidenceV2Archive, resultErr error) {
	archive, err := openReleaseEvidenceV2ArchiveHistory(ctx, options)
	if err != nil {
		return nil, err
	}
	if err := archive.readIntents(ctx, options.Adoption); err != nil {
		return nil, errors.Join(err, archive.Close())
	}
	return archive, archive.owner.check(ctx)
}

func openReleaseEvidenceV2ArchiveHistory(ctx context.Context, options ReleaseEvidenceV2ArchiveOptions) (result *ReleaseEvidenceV2Archive, resultErr error) {
	owner, err := newReleaseEvidenceV2ArchiveOwner(ctx, options)
	if err != nil {
		return nil, err
	}
	archive := &ReleaseEvidenceV2Archive{owner: owner}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			resultErr = errors.Join(resultErr, archive.Close())
			result = nil
		}
	}()
	history := &releaseEvidenceV2StartupHistory{cfg: owner.cfg, initial: map[uint64]ReleaseEvidenceV2ActivationContext{}, keys: map[uint64]map[byte]ed25519.PublicKey{}, current: map[uint64]releaseEvidenceV2StartupCursor{}, lastOrdinary: map[uint64]*releaseMeasurementInputJournal{}, lastNative: map[uint64]*releaseMeasurementInputJournal{}, lastOrdinaryBefore: map[uint64]releaseEvidenceV2StartupCursor{}, inputByEpoch: map[uint64]map[uint64]*releaseMeasurementInputJournal{}, terminals: map[uint64]*AttemptSettlementClosureV2{}, archive: owner}
	archive.history = history
	bounds := owner.cfg.EvidenceV2.Bounds
	for index, operator := range owner.cfg.EvidenceV2.Operators {
		fields := []struct {
			name string
			ref  ReleaseEvidenceV2File
		}{{"activation", operator.Activation}, {"vpk-signature", operator.VPKSignature}, {"hotkey-signature", operator.HotkeySignature}, {"context", operator.Context}, {"history", operator.History}}
		values := map[string][]byte{}
		for _, field := range fields {
			encoded, err := owner.read(ctx, ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: fmt.Sprintf("activation/no-%d/%s", operator.NoID, field.name)}, bounds.MaxControlBytes)
			if err != nil {
				return nil, err
			}
			if uint64(len(encoded)) != field.ref.Bytes || attemptHex32(sha256.Sum256(encoded)) != field.ref.SHA256 {
				return nil, errors.New("archive activation source differs from its approved original file pin")
			}
			values[field.name] = encoded
		}
		initial, err := decodeReleaseEvidenceV2ActivationContext(values["context"], bounds.Cut.MaxHeaderBytes)
		if err != nil {
			return nil, err
		}
		activation, err := protocol.DecodeValidatorEvidenceActivationPayload(values["activation"])
		if err != nil {
			return nil, err
		}
		if activation != initial.Activation || activation.Hotkey != options.Hotkey || activation.NoID != operator.NoID || index > 0 && operator.NoID <= owner.cfg.EvidenceV2.Operators[index-1].NoID {
			return nil, errors.New("archive activation identity or configured order differs")
		}
		if err := validateReleaseMeasurementInputV2Context(&owner.cfg, operator.NoID, initial.InitialCut); err != nil {
			return nil, err
		}
		if err := activation.Verify(initial.Activation, values["vpk-signature"], values["hotkey-signature"]); err != nil {
			return nil, err
		}
		var origin string
		for _, configured := range owner.cfg.Operators {
			if configured.NoID == operator.NoID {
				origin = configured.APIURL
			}
		}
		encoded, err := owner.read(ctx, ReleaseEvidenceV2CaptureSource{Kind: "server-keys", Name: fmt.Sprintf("no-%d", operator.NoID), Origin: origin}, bounds.MaxControlBytes)
		if err != nil {
			return nil, err
		}
		keys, err := decodeReleaseServerKeysV2(encoded, bounds.MaxControlBytes)
		if err != nil {
			return nil, err
		}
		history.keys[operator.NoID], history.initial[operator.NoID] = keys, initial
		history.participants = append(history.participants, AttemptSettlementRuntimeV2Participant{NoID: operator.NoID})
		archive.inputs = append(archive.inputs, releaseEvidenceV2ActivationInput{Config: operator, Context: initial, Candidate: activation, VPKSignature: values["vpk-signature"], HotkeySignature: values["hotkey-signature"], HistoryBytes: values["history"]})
		if err := owner.newLedger(ctx, initial.InitialCut); err != nil {
			return nil, err
		}
	}
	history.activationHistory, err = replayReleaseEvidenceV2ActivationHistories(ctx, &owner.cfg, archive.inputs, history.keys)
	if err != nil {
		return nil, err
	}
	// Import only already verified legacy records into the lifetime trail index.
	// The original canonical closure bytes and all V1 checks remain unchanged.
	var initialHistory ReleaseEvidenceV2ActivationHistory
	if err := decodeAttemptStreamV2JSON(archive.inputs[0].HistoryBytes, bounds.MaxHistoryBytes, &initialHistory); err != nil {
		return nil, err
	}
	for _, raw := range initialHistory.LegacyClosures {
		var closure AttemptSettlementClosure
		if err := decodeAttemptStreamV2JSON(raw, bounds.MaxHistoryBytes, &closure); err != nil {
			return nil, err
		}
		for _, transition := range closure.Transitions {
			cut := transition.PreFold.AttemptCut
			if cut == nil {
				return nil, errors.New("archive legacy history omits its signed record prefix")
			}
			ledger := owner.ledgers[transition.Identity.NoID]
			if ledger == nil {
				return nil, errors.New("archive legacy history contains an unconfigured ledger")
			}
			compact := AttemptCutV2{Context: AttemptCutV2Context{Identity: cut.Identity, FirstSequence: cut.FirstSequence, PriorRoot: cut.PriorRoot}, LastSequence: cut.LastSequence, Root: cut.Root}
			visitor, err := owner.beginCut(ctx, transition.Identity.NoID, compact)
			if err != nil {
				return nil, err
			}
			for _, record := range cut.Records {
				if err := visitor.visit(record); err != nil {
					return nil, err
				}
			}
			if err := visitor.finish(ctx); err != nil {
				return nil, err
			}
		}
	}
	for index, input := range archive.inputs {
		initial := input.Context.InitialCut
		cursor := releaseEvidenceV2StartupCursor{epoch: initial.Boundary.SettlementEpoch, first: initial.FirstSequence, egressFirst: initial.EgressFirstSequence, generation: initial.EgressGeneration, priorRoot: initial.PriorRoot, lastSequence: initial.FirstSequence - 1, lastRoot: initial.PriorRoot, lastBoundary: initial.Boundary}
		if history.activationHistory != nil {
			cursor.prior = slices.Clone(history.activationHistory.Transitions[index].PostFold)
		}
		history.current[input.Config.NoID] = cursor
		if err := history.matchLedgerCut(ctx, input.Config.NoID, cursor.lastSequence, cursor.lastRoot); err != nil {
			return nil, err
		}
	}
	var ordinary []*releaseMeasurementInputJournal
	var closures []*AttemptSettlementClosureV2
	remaining := bounds.MaxHistoryBytes
	for _, source := range slices.SortedFunc(maps.Keys(owner.sources), func(left, right ReleaseEvidenceV2CaptureSource) int {
		return strings.Compare(left.Kind+"\x00"+left.Origin+"\x00"+left.Name, right.Kind+"\x00"+right.Origin+"\x00"+right.Name)
	}) {
		if source.Kind != "private" {
			continue
		}
		input := strings.HasPrefix(source.Name, "measurements/inputs/")
		terminal := strings.HasPrefix(source.Name, "settlement-closures-v2/")
		if !input && !terminal {
			continue
		}
		maximum := bounds.MaxInputJournalBytes
		if terminal {
			maximum = bounds.MaxClosureBytes
		}
		encoded, err := owner.read(ctx, source, min(maximum, remaining))
		if err != nil {
			return nil, err
		}
		remaining -= uint64(len(encoded))
		name := filepath.Base(source.Name)
		if input {
			if source.Name != "measurements/inputs/"+name {
				return nil, errors.New("archive input namespace is noncanonical")
			}
			epoch, noID, legacy, err := releaseEvidenceV2HistoryInputName(name)
			if err != nil {
				return nil, err
			}
			if legacy {
				return nil, errors.New("archive V2 ordinary history cannot reinterpret legacy inputs")
			}
			journal, err := history.decodeInput(ctx, releaseEvidenceV2HistoryFile{name: name, epoch: epoch, noID: noID, encoded: encoded})
			if err != nil {
				return nil, err
			}
			if history.initial[noID].Schema == "" {
				return nil, errors.New("archive input is outside the configured operator census")
			}
			if history.inputByEpoch[epoch] == nil {
				history.inputByEpoch[epoch] = map[uint64]*releaseMeasurementInputJournal{}
			}
			if history.inputByEpoch[epoch][noID] != nil {
				return nil, errors.New("archive repeats a native input identity")
			}
			history.inputByEpoch[epoch][noID] = journal
			ordinary = append(ordinary, journal)
		} else {
			epoch, err := strconv.ParseUint(strings.TrimSuffix(name, ".json"), 10, 64)
			if err != nil || source.Name != "settlement-closures-v2/"+strconv.FormatUint(epoch, 10)+".json" {
				return nil, errors.New("archive terminal namespace is noncanonical")
			}
			closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, encoded, bounds.MaxClosureBytes, bounds.MaxParticipants)
			if err != nil || closure.Epoch != epoch {
				return nil, errors.Join(errors.New("archive terminal epoch differs"), err)
			}
			closures = append(closures, closure)
		}
	}
	slices.SortFunc(ordinary, func(left, right *releaseMeasurementInputJournal) int {
		for _, pair := range [][2]uint64{{left.MeasurementInput.SettlementEpoch, right.MeasurementInput.SettlementEpoch}, {left.SubnetEpoch, right.SubnetEpoch}, {left.MeasurementInput.NoID, right.MeasurementInput.NoID}} {
			if pair[0] < pair[1] {
				return -1
			}
			if pair[0] > pair[1] {
				return 1
			}
		}
		return 0
	})
	slices.SortFunc(closures, func(left, right *AttemptSettlementClosureV2) int {
		if left.Epoch < right.Epoch {
			return -1
		}
		if left.Epoch > right.Epoch {
			return 1
		}
		return 0
	})
	next := 0
	for _, closure := range closures {
		for next < len(ordinary) && ordinary[next].MeasurementInput.SettlementEpoch <= closure.Epoch {
			if err := history.replayOrdinary(ctx, ordinary[next]); err != nil {
				return nil, err
			}
			next++
		}
		if err := history.replayTerminal(ctx, closure); err != nil {
			return nil, err
		}
	}
	for ; next < len(ordinary); next++ {
		if err := history.replayOrdinary(ctx, ordinary[next]); err != nil {
			return nil, err
		}
	}
	return archive, owner.check(ctx)
}

func (self *ReleaseEvidenceV2Archive) readIntents(ctx context.Context, request *ReleaseHistoryAdoptionV2) error {
	owner, history := self.owner, self.history
	bounds := owner.cfg.EvidenceV2.Bounds
	raw, err := owner.read(ctx, ReleaseEvidenceV2CaptureSource{Kind: "private", Name: "steering-intents.json"}, bounds.IntentFileLimit())
	if err != nil {
		return err
	}
	var file steeringIntentFile
	if err := decodeAttemptStreamV2JSON(raw, bounds.IntentFileLimit(), &file); err != nil {
		return err
	}
	canonical, err := marshalAttemptSettlementV2JSON(ctx, &file, bounds.IntentFileLimit(), true, true)
	if err != nil || file.Schema != steeringIntentSchema || !bytes.Equal(canonical, raw) {
		return errors.Join(errors.New("archive original intent store is not canonical"), err)
	}
	if request != nil {
		copy := *request
		encoded, err := json.MarshalIndent(copy, "", "  ")
		if err != nil {
			return err
		}
		encoded = append(encoded, '\n')
		if _, err := DecodeReleaseHistoryAdoptionV2(encoded, ReleaseMeasurementContentHash(encoded)); err != nil {
			return err
		}
		if copy.DeploymentID != owner.cfg.DeploymentID || copy.ValidatorID != owner.cfg.ValidatorID || copy.CoordinatorStateDir != owner.cfg.StateDir {
			return errors.New("archive adoption differs from its authenticated deployment namespace")
		}
		self.adoption, err = copy.matchPrefix(ctx, &file, raw, bounds.IntentFileLimit())
		if err != nil {
			return err
		}
		history.historyAdoption = self.adoption
	}
	var wire struct {
		Schema  string            `json:"schema"`
		Current json.RawMessage   `json:"current"`
		History []json.RawMessage `json:"history"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	all := append([]json.RawMessage(nil), wire.History...)
	if len(wire.Current) != 0 && !bytes.Equal(bytes.TrimSpace(wire.Current), []byte("null")) {
		all = append(all, wire.Current)
	}
	if len(all) == 0 {
		return errors.New("archive has no actual native intent history")
	}
	var previous *SteeringIntent
	var priorArtifact *ReleaseMeasurementArtifact
	seen := map[string]bool{}
	for index, encoded := range all {
		var intent SteeringIntent
		if err := decodeAttemptStreamV2JSON(encoded, bounds.IntentFileLimit(), &intent); err != nil {
			return err
		}
		if err := validateSteeringIntentLifecycle(&intent, index < len(wire.History)); err != nil {
			return err
		}
		if err := intent.VerifyVectorHash(); err != nil {
			return err
		}
		if seen[intent.VectorHash] {
			return errors.New("archive repeats an original native vector")
		}
		seen[intent.VectorHash] = true
		allowGap := self.adoption.allowsIntentEdge(previous, &intent)
		if previous != nil {
			if err := validateSteeringIntentSuccessorWithGapsV2(previous, &intent, allowGap); err != nil {
				return err
			}
		}
		custody := &releaseEvidenceV2StartupReferences{remaining: bounds.MaxHistoryBytes, archive: owner}
		measurement, err := history.readContentReference(ctx, custody, intent.MeasurementArtifactPath, intent.MeasurementArtifactHash, intent.MeasurementArtifactSize, bounds.MaxArtifactBytes, false)
		if err != nil {
			return err
		}
		envelopeBytes, err := history.readContentReference(ctx, custody, intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize, bounds.MaxControlBytes, true)
		if err != nil {
			return err
		}
		artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, bounds.MaxArtifactBytes, bounds.MaxOperators)
		if err != nil {
			return err
		}
		envelope, err := DecodeReleaseMeasurementEnvelopeV2(ctx, envelopeBytes, bounds.MaxControlBytes)
		if err != nil {
			return err
		}
		if err := history.matchIntentReference(ctx, &intent, artifact, envelope); err != nil {
			return err
		}
		if previous == nil {
			if artifact.PreviousArtifactHash != "" {
				return errors.New("archive omits the first signed measurement predecessor")
			}
		} else {
			if artifact.PreviousArtifactHash != previous.MeasurementArtifactHash || !allowGap && !consecutiveNativeSettlementGapV2(priorArtifact, artifact) && artifact.SettlementEpoch > priorArtifact.SettlementEpoch+1 {
				return errors.New("archive measurement predecessor or permitted clocks differ")
			}
			if err := verifyReleaseMeasurementHeadLineage(priorArtifact, artifact); err != nil {
				return err
			}
		}
		self.intents = append(self.intents, ReleaseEvidenceV2CapturedIntent{Sequence: uint64(index + 1), Encoded: bytes.Clone(encoded), Intent: intent, Measurement: measurement, Envelope: envelopeBytes})
		previous, priorArtifact = &self.intents[len(self.intents)-1].Intent, artifact
	}
	return owner.check(ctx)
}
