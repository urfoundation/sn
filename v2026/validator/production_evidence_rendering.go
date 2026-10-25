// A signed production config can name its activation inputs before they can
// exist. Rendering them is a separate successor that the original approval key
// re-approves; the signed original is never rewritten or run.
package validator

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/urfoundation/sn/v2026/protocol"
)

// ErrReleaseProductionActivationPending names a signed schema-3 config whose
// evidence_v2 references are activation-pending. Its approval binds exactly
// those unrendered references, so neither the producer nor the activation flow
// rewrites it; only the authenticated rendered successor can run.
var ErrReleaseProductionActivationPending = errors.New("signed production config has activation-pending evidence_v2 inputs and is never run or rewritten in place: run `validator activate --config=<path> --rendered-config=<new path> --successor-approval=<path> --original-authority=<path>`, have the approval key sign the successor approval it prints, then start `validator run --config=<new path>`")

// Five input paths and two scratch roots, in Files() order then replay/seal.
func releaseEvidenceV2DeclaredPaths(operator ReleaseEvidenceV2OperatorConfig) []string {
	paths := make([]string, 0, 7)
	for _, file := range operator.Files() {
		paths = append(paths, file.Path)
	}
	return append(paths, operator.ReplayScratchRoot, operator.SealScratchRoot)
}

// A signed production census is pending only as a whole: every entry is
// unrendered and pre-declares every path, so bootstrap custody checks reserve
// exactly the namespaces that rendering later fills and nothing else.
func requireReleaseEvidenceV2ProductionPending(operators []ReleaseEvidenceV2OperatorConfig) error {
	if len(operators) == 0 {
		return errors.New("activation-pending production evidence has no operator census")
	}
	rendered := 0
	for _, operator := range operators {
		if !operator.Unrendered() {
			rendered++
			continue
		}
		if slices.Contains(releaseEvidenceV2DeclaredPaths(operator), "") {
			return errors.New("activation-pending production evidence must pre-declare every input path and both scratch roots")
		}
	}
	if rendered == len(operators) {
		return errors.New("production evidence_v2 inputs are already rendered; this config has no pre-activation purpose")
	}
	if rendered != 0 {
		return errors.New("production evidence_v2 census mixes rendered and activation-pending operators")
	}
	return nil
}

// Every producer rule applies except that the complete census is pending. The
// authority capsule, runtime and authority history must already be loaded.
func (c ReleaseConfig) validateProductionPreActivation() error {
	if c.SchemaVersion != ReleaseMainnetProductionSchemaVersion {
		return errors.New("production pre-activation requires an authenticated schema 3 config")
	}
	if err := requireReleaseEvidenceV2ProductionPending(c.EvidenceV2.Operators); err != nil {
		return err
	}
	return c.validateWithMode(false, false, true, false)
}

// Only a pending predecessor whose census changed selects the rendering class.
// Equal censuses, pending or rendered, keep the original continuity rule.
func productionEvidenceRenderingTransition(original, current *ReleaseConfig) bool {
	return slices.ContainsFunc(original.EvidenceV2.Operators, ReleaseEvidenceV2OperatorConfig.Unrendered) &&
		!reflect.DeepEqual(original.EvidenceV2.Operators, current.EvidenceV2.Operators)
}

// Clears exactly what rendering and its re-approval change: each reference's
// length and digest, the appended authority history and the approval's own
// content address. Paths, scratch roots, signer and every other field remain.
func productionEvidenceRenderingProjection(cfg *ReleaseConfig) ([]byte, error) {
	selected := productionEconomicSelection(cfg)
	if selected == nil {
		return nil, errors.New("evidence rendering has no production approval selection")
	}
	owned := *cfg
	owned.EvidenceV2.Operators = slices.Clone(cfg.EvidenceV2.Operators)
	for index := range owned.EvidenceV2.Operators {
		operator := &owned.EvidenceV2.Operators[index]
		for _, file := range []*ReleaseEvidenceV2File{&operator.Activation, &operator.VPKSignature, &operator.HotkeySignature, &operator.Context, &operator.History} {
			file.Bytes, file.SHA256 = 0, ""
		}
	}
	owned.ProductionAuthorityHistory = nil
	selection := &ReleaseOwnerRecycleApprovalConfig{Signer: selected.Signer}
	if cfg.TreasuryApproval != nil {
		owned.TreasuryApproval = selection
	} else {
		owned.OwnerRecycleApproval = selection
	}
	return json.Marshal(&owned)
}

// The terms a rendering re-approval can never move: everything except the
// complete-config hash and the drained activation boundary. The window's end,
// census, owners, limits, runtime and economic policy all stay the original's.
func productionRenderingFixedTerms(approval OwnerRecycleApproval) string {
	approval.FirstNativeEpoch, approval.ValidFromNativeBlock = 0, 0
	if approval.Production != nil {
		production := *approval.Production
		production.ActivationNativeHash, production.ActivationNativeBlock = [32]byte{}, 0
		approval.Production = &production
	}
	return productionCapacityEconomicHash(approval)
}

// Rendering waits at least one coordinator epoch after the original was signed,
// so the original's drained activation epoch has long passed when the first
// decision can be made. The re-approval either keeps that boundary exactly or
// advances it to one later drained native boundary: a strictly later epoch and
// block, the activation block equal to the new valid_from in the original's
// wire form, and still inside the original's unchanged block and epoch window.
// That block's finalized facts are read by activate before it asks for the
// signature and re-read at every decision; this rule needs no chain.
func validateProductionRenderingApproval(prior, next OwnerRecycleApproval) error {
	if prior.Production == nil || next.Production == nil {
		return errors.New("evidence rendering has no production approval window")
	}
	if productionRenderingFixedTerms(prior) != productionRenderingFixedTerms(next) {
		return errors.New("evidence rendering changes the original economic approval")
	}
	if next.FirstNativeEpoch == prior.FirstNativeEpoch && next.ValidFromNativeBlock == prior.ValidFromNativeBlock &&
		next.Production.ActivationNativeHash == prior.Production.ActivationNativeHash && next.Production.ActivationNativeBlock == prior.Production.ActivationNativeBlock {
		return nil
	}
	activation := ownerRecycleActivationBlock(&next)
	if next.FirstNativeEpoch <= prior.FirstNativeEpoch || next.ValidFromNativeBlock <= prior.ValidFromNativeBlock ||
		activation != next.ValidFromNativeBlock || activation <= ownerRecycleActivationBlock(&prior) ||
		(next.Production.ActivationNativeBlock == 0) != (prior.Production.ActivationNativeBlock == 0) ||
		next.Production.ActivationNativeHash == ([32]byte{}) || next.Production.ActivationNativeHash == prior.Production.ActivationNativeHash ||
		next.ValidFromNativeBlock > next.ValidThroughNativeBlock || next.FirstNativeEpoch > next.Production.ValidThroughNativeEpoch {
		return errors.New("evidence rendering may only advance its signed drained activation to a later native epoch and block inside the original window")
	}
	return nil
}

// A rendering successor pins every pending reference at its pre-declared path
// and changes nothing else in the config. Its approval retains the original
// verbatim except the complete-config hash and, optionally, one later drained
// activation boundary. Callers authenticate both envelopes; the preview applies
// this same rule to its unsigned successor.
func validateProductionEvidenceRendering(original, current *ReleaseConfig, prior, next OwnerRecycleApproval) error {
	if original == nil || current == nil || prior.Production == nil || next.Production == nil {
		return errors.New("evidence rendering has no authenticated production predecessor")
	}
	if err := requireReleaseEvidenceV2ProductionPending(original.EvidenceV2.Operators); err != nil {
		return err
	}
	if len(current.EvidenceV2.Operators) != len(original.EvidenceV2.Operators) {
		return errors.New("evidence rendering changes the operator census")
	}
	bounds := current.EvidenceV2.Bounds
	limits := []uint64{uint64(protocol.ValidatorEvidenceActivationPayloadSize), 64, 64, bounds.Cut.MaxHeaderBytes, bounds.MaxHistoryBytes}
	for index, operator := range current.EvidenceV2.Operators {
		declared := original.EvidenceV2.Operators[index]
		if operator.NoID != declared.NoID || operator.ReplayScratchRoot != declared.ReplayScratchRoot || operator.SealScratchRoot != declared.SealScratchRoot {
			return errors.New("evidence rendering changes an operator or its scratch roots")
		}
		if operator.Activation.Bytes != limits[0] || operator.VPKSignature.Bytes != 64 || operator.HotkeySignature.Bytes != 64 {
			return errors.New("evidence rendering pins a reference of the wrong exact width")
		}
		for fileIndex, file := range operator.Files() {
			if file.Path != declared.Files()[fileIndex].Path {
				return errors.New("evidence rendering moves a reference from its pre-declared path")
			}
			if err := file.Validate(limits[fileIndex]); err != nil {
				return err
			}
		}
	}
	before, beforeErr := productionEvidenceRenderingProjection(original)
	after, afterErr := productionEvidenceRenderingProjection(current)
	if err := errors.Join(beforeErr, afterErr); err != nil {
		return err
	}
	if !bytes.Equal(before, after) {
		return errors.New("evidence rendering changes signed configuration beyond its rendered references")
	}
	return validateProductionRenderingApproval(prior, next)
}
