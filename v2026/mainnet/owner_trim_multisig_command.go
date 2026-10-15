// Host commands for a native multisig owner. Every phase runs inside the
// original owner-trim custody; the selector names the latest signatory step.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Parsed public inputs. Paths were already checked against the fixed journal.
type ownerTrimMultisigOptions struct {
	step               int
	metadataPath       string
	ledgerMetadata     string
	templatePath       string
	stepConfigPath     string
	requestPath        string
	requestHash        string
	replyPath          string
	replyHash          string
	submissionPath     string
	submissionHash     string
	submissionKey      string
	pruningPolicy      string
	registrationPolicy string
}

// Each mode either prints its exact portable artifact or the retained status.
func runOwnerTrimMultisigMode(ctx context.Context, mode string, options ownerTrimMultisigOptions, store *ownerTrimStore, config ownerTrimExecutionConfig, key string, encoder *json.Encoder, stderr io.Writer) int {
	owner := &ownerTrimMultisigOwner{store: store, config: config, key: key}
	_, steps, err := owner.load()
	if err != nil {
		fmt.Fprintln(stderr, "trim multisig retained operation:", err)
		return 3
	}
	stepless := mode == "trim-apply" || mode == "trim-resume" || mode == "trim-multisig-plan" || mode == "trim-multisig-apply"
	if stepless != (options.step < 0) || mode == "trim-import" {
		fmt.Fprintln(stderr, "trim multisig phases import only portable replies and require --multisig-step naming the latest step for step effects")
		return 2
	}
	result := 0
	switch mode {
	case "trim-multisig-plan":
		state, err := ownerTrimMultisigSequence(config, key, steps)
		if err != nil || !state.Open || !state.Settled || len(steps) >= ownerTrimMultisigStepLimit {
			fmt.Fprintln(stderr, "trim multisig planning requires the original open operation with every earlier step settled:", err)
			return 3
		}
		raw, _, err := readBootstrapRootFile(ctx, options.templatePath, maxRpcReplyBytes)
		var template ownerTrimMultisigStepTemplate
		if err == nil {
			err = decodePlanJson(raw, &template)
		}
		var stepConfig ownerTrimExecutionConfig
		if err == nil {
			stepConfig, err = ownerTrimMultisigStepConfig(config, template, *state.Timepoint)
		}
		metadata, _, readErr := readBootstrapRootFile(ctx, options.metadataPath, 2*maxMetadataRpcReplyBytes+3)
		if err == nil {
			err = readErr
		}
		if err == nil {
			stepConfig.Action, err = prepareOwnerTrimAction(stepConfig.Action, strings.TrimSpace(string(metadata)))
		}
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig step template:", err)
			return 2
		}
		chain, err := newOwnerTrimCanonicalChain(config, key, store.policy, nil)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig owned observation route:", err)
			return 2
		}
		defer chain.client.httpClient.CloseIdleConnections()
		observed, err := chain.planMultisigStep(ctx, stepConfig.Action, config.Action.Multisig.Signatory)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig pending operation readback refuses this step:", err)
			return 3
		}
		if _, _, err := owner.load(); err != nil {
			fmt.Fprintln(stderr, "trim multisig planning custody changed:", err)
			return 3
		}
		if err := encoder.Encode(struct {
			Config       ownerTrimExecutionConfig         `json:"step_config"`
			SigningBytes string                           `json:"approval_signing_bytes"`
			Observed     ownerTrimMultisigPlanObservation `json:"observed_pending_operation"`
		}{Config: stepConfig, SigningBytes: "0x" + hex.EncodeToString(stepConfig.signingBytes()), Observed: observed}); err != nil {
			fmt.Fprintln(stderr, "trim multisig plan output:", err)
			return 1
		}
		return 0
	case "trim-multisig-apply":
		raw, _, err := readBootstrapRootFile(ctx, options.stepConfigPath, maxRpcReplyBytes)
		var stepConfig ownerTrimExecutionConfig
		if err == nil {
			err = decodePlanJson(raw, &stepConfig)
		}
		if err == nil {
			err = owner.appendStep(stepConfig)
		}
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig step approval or original custody refuses this step:", err)
			return 3
		}
	case "trim-export":
		step, err := ownerTrimMultisigLatest(steps, options.step)
		if err == nil && (step.Phase != "reserved" || step.Signature != "") {
			err = errors.New("step is not in original reserved custody; recover any existing or uncertain signature")
		}
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig export:", err)
			return 3
		}
		metadata, _, err := readBootstrapRootFile(ctx, options.metadataPath, 2*maxMetadataRpcReplyBytes+3)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig export metadata:", err)
			return 2
		}
		request, err := newOwnerSigningRequest(step.Config, key, strings.TrimSpace(string(metadata)), options.ledgerMetadata)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig export request:", err)
			return 2
		}
		if _, _, err := owner.load(); err != nil {
			fmt.Fprintln(stderr, "trim multisig export original custody changed:", err)
			return 3
		}
		if err := encoder.Encode(request); err != nil {
			fmt.Fprintln(stderr, "trim multisig export output:", err)
			return 1
		}
		return 0
	case "trim-import-reply":
		step, err := ownerTrimMultisigLatest(steps, options.step)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig reply:", err)
			return 3
		}
		action := step.Config.Action
		request, err := readOwnerSigningRequest(ctx, options.requestPath, ownerSigningTrust{RequestHash: options.requestHash, ApprovalKey: key,
			Owner: action.Coldkey, Genesis: action.Network.GenesisHash, Signatory: action.Multisig.Signatory})
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig reply request differs from the latest approved step:", err)
			return 3
		}
		reply, err := readOwnerSigningReply(ctx, options.replyPath, options.replyHash)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig original signatory reply:", err)
			return 2
		}
		if err := owner.importReply(options.step, request, reply); err != nil {
			fmt.Fprintln(stderr, "trim multisig signature cannot replace original custody or resolve another step:", err)
			return 3
		}
	case "trim-submit-plan":
		approval, err := owner.approvalTemplate(options.step)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig submission planning:", err)
			return 3
		}
		if options.pruningPolicy != ownerTrimRequirePruningImmunity && options.pruningPolicy != ownerTrimAcceptPruningRisk ||
			options.registrationPolicy != ownerTrimRequireClosedRegistration && options.registrationPolicy != ownerTrimAcceptRegistrationRisk {
			fmt.Fprintln(stderr, "trim multisig submission planning requires explicit supported pruning and registration policy choices")
			return 2
		}
		approval.PublicPruningPolicy, approval.RegistrationPolicy = options.pruningPolicy, options.registrationPolicy
		approval.ResidualRisks = approval.residuals()
		if _, _, err := owner.load(); err != nil {
			fmt.Fprintln(stderr, "trim multisig submission planning custody changed:", err)
			return 3
		}
		if err := encoder.Encode(struct {
			Approval     ownerTrimBestEffortApproval `json:"approval_template"`
			SigningBytes string                      `json:"signing_bytes"`
		}{Approval: approval, SigningBytes: "0x" + hex.EncodeToString(approval.signingBytes())}); err != nil {
			fmt.Fprintln(stderr, "trim multisig submission policy output:", err)
			return 1
		}
		return 0
	case "trim-submit", "trim-reconcile":
		step, err := ownerTrimMultisigLatest(steps, options.step)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig step:", err)
			return 3
		}
		chain, err := newOwnerTrimCanonicalChain(step.Config, key, store.policy, nil)
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig owned observation route:", err)
			return 2
		}
		defer chain.client.httpClient.CloseIdleConnections()
		if mode == "trim-reconcile" {
			_, _, err = owner.reconcile(ctx, chain, options.step)
		} else {
			raw, digest, readErr := readBootstrapRootFile(ctx, options.submissionPath, maxRpcReplyBytes)
			var approval ownerTrimBestEffortApproval
			err = readErr
			if err == nil && digest != options.submissionHash {
				err = errors.New("submission policy file differs from its exact sha256 pin")
			}
			if err == nil {
				err = decodePlanJson(raw, &approval)
			}
			if err != nil {
				fmt.Fprintln(stderr, "trim multisig submission policy input:", err)
				return 2
			}
			err = owner.submit(ctx, chain, options.step, approval, options.submissionKey)
		}
		if err != nil {
			fmt.Fprintln(stderr, "trim multisig reconciliation unresolved or effects blocked; retain original custody:", err)
			result = 1
		}
	}
	record, steps, err := owner.load()
	if err != nil {
		fmt.Fprintln(stderr, "trim multisig retained result:", err)
		return 1
	}
	status, err := ownerTrimMultisigSummary(config, key, steps)
	if err != nil {
		fmt.Fprintln(stderr, "trim multisig retained result:", err)
		return 1
	}
	if err := encoder.Encode(struct {
		Multisig ownerTrimMultisigStatus `json:"multisig"`
		Record   ownerTrimRecord         `json:"retained_action"`
	}{Multisig: status, Record: record}); err != nil {
		fmt.Fprintln(stderr, "trim multisig result output; resume original custody:", err)
		return 1
	}
	return result
}
