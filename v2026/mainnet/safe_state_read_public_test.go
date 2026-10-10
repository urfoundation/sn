// The real public current-policy command must refuse a canceled final pending
// read without consuming an attempt, then reuse its exact original custody.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"math/big"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Genuine native proof and the separate synthetic v2 signature select the
// production route. No test provenance capability is supplied to the command.
func TestSafeCurrentPublicReadFailurePreservesOriginalAttempt(t *testing.T) {
	f := newBootstrapSuccessorCanonicalFixture(t)
	chain, plan := f.original.contracts, f.approval.Plan
	model := &bootstrapSuccessorExecutionFixture{t: t, approval: f.approval, key: f.key}
	revision := bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, bootstrapSuccessorSafeCurrentTestNext(t, model, f.canonical, bootstrapSuccessorRuntimeHistory{}, nil), f.key)
	reference := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-read-current-acceptance.json"), revision)
	var mode string
	var cancelRead context.CancelFunc
	reached := 0
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		oracle := &safeExecutionFixture{state: chain.state, transaction: plan.transaction(), owners: slices.Clone(plan.Request.Owners)}
		proof := safeCurrentProofFixtureFromOracle(t, oracle, plan.Review.Request.Version, plan.Review.Request.Variant)
		proof.entries[":code"] = slices.Clone(chain.code)
		chain.advanceEmpty()
		witness := bootstrapSuccessorSafeCurrentTestHeadWithLock(t, chain, proof.entries)
		chain.nativeProof = func(params []any) any {
			if len(params) != 2 || params[1] != witness.At {
				return mappingFixtureRpcError{code: -32000}
			}
			return map[string]any{"at": witness.At, "proof": witness.Nodes}
		}
		prior := chain.override
		chain.override = func(method string, params []any, result any) any {
			result = prior(method, params, result)
			if mode != "" && method == "eth_getStorageAt" && len(params) == 3 && params[0] == plan.Review.Transaction.Safe.Hex() && params[1] == common.BigToHash(big.NewInt(8)).Hex() && params[2] == "pending" {
				reached++
				if mode == "cancel" {
					cancelRead()
					return nil
				}
				return common.Hash{31: 93}.Hex()
			}
			return result
		}
	}()
	args := []string{"contract-successor-execution-resume", "--config", f.original.path, "--run-dir", f.original.config.RunDirectory, "--accept-plan-hash", f.original.preparation.Plan.ContentHash}
	args = append(append(args, f.paths...), f.approvalArgs...)
	args = append(args, "--online", "--canonical-approval", f.canonicalRef.Path, "--canonical-approval-sha256", f.canonicalRef.Sha256,
		"--safe-current-revision", reference.Path, "--safe-current-revision-sha256", reference.Sha256)
	invoke := func(ctx context.Context, submit bool) (int, []byte, string) {
		var output, diagnostic bytes.Buffer
		input := slices.Clone(args)
		if submit {
			input = append(input, "--submit", "--accept-safe-current-policy", rootObjectHash(revision))
		}
		code := runBootstrapSuccessorExecutionCommand(f.original.storageContext(ctx), input, &output, &diagnostic)
		return code, output.Bytes(), diagnostic.String()
	}
	if code, _, diagnostic := invoke(t.Context(), false); code != 0 {
		t.Fatal("actual current-policy import failed", code, diagnostic)
	}
	before := bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)
	nonces := bootstrapSuccessorPreparationTestFiles(t, plan.Request.RegistryDirectory)
	for _, fault := range []string{"cancel", "conflict"} {
		ctx, cancel := context.WithCancel(t.Context())
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			mode, cancelRead, reached = fault, cancel, 0
		}()
		code, output, diagnostic := invoke(ctx, true)
		cancel()
		var observations, sends int
		func() {
			chain.stateLock.Lock()
			defer chain.stateLock.Unlock()
			mode = ""
			observations, sends = reached, len(chain.writes)
		}()
		if code == 0 || len(output) != 0 || observations != 1 || sends != 8 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, f.original.config.RunDirectory)) || !maps.Equal(nonces, bootstrapSuccessorPreparationTestFiles(t, plan.Request.RegistryDirectory)) {
			t.Fatal("failed actual final pending read changed original allowance/custody", fault, code, diagnostic, observations, sends)
		}
		if fault == "cancel" {
			if !strings.Contains(diagnostic, context.Canceled.Error()) || !strings.Contains(diagnostic, errRpcObservationUnavailable.Error()) || strings.Contains(diagnostic, errRpcIntegrity.Error()) || strings.Contains(diagnostic, "Safe scoped pending authority word changed") {
				t.Fatal("canceled public state observation asserted changed authority", diagnostic)
			}
		} else if !strings.Contains(diagnostic, errRpcIntegrity.Error()) || !strings.Contains(diagnostic, "Safe scoped pending authority word changed") {
			t.Fatal("positive returned authority conflict lost its hard refusal", diagnostic)
		}
	}
	code, output, diagnostic := invoke(t.Context(), true)
	var result bootstrapSuccessorExecutionResult
	if code != 0 || json.Unmarshal(output, &result) != nil || result.CumulativeAttempts != 9 || !result.SubmissionAttempted {
		t.Fatal("same original public owner could not recover through exact approved send", code, result, diagnostic)
	}
	func() {
		chain.stateLock.Lock()
		defer chain.stateLock.Unlock()
		if len(chain.writes) != 9 || !bytes.Equal(chain.writes[8], common.FromHex(plan.SignedRelayer)) {
			t.Fatal("public recovery replaced original transaction bytes or duplicated send")
		}
	}()
}
