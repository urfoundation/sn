//go:build linux || darwin

// Restore selects exact original snapshot owners after checking the complete
// retained Claim history. Target publication remains a separate reviewed action.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorClaimRestoreRequestSchema = "urnetwork-claim-monitor-restore-request-v1"

type monitorClaimRestoreRequest struct {
	Schema      string                           `json:"schema"`
	Expected    identityExpectation              `json:"expected"`
	Policy      monitorClaimPolicy               `json:"policy"`
	Original    monitorHistoryReference          `json:"original"`
	Preparation durablevolume.PreparationRequest `json:"preparation"`
}

// Logical paths, independent co-owners and original approvals remain unchanged.
// The result authorizes neither signing nor automatic service restart.
func buildMonitorClaimRestoreRequest(ctx context.Context, request monitorClaimRestoreRequest) (durablevolume.PreparationRequest, error) {
	var empty durablevolume.PreparationRequest
	if err := durablepath.Require(ctx); err != nil {
		return empty, err
	}
	if request.Schema != monitorClaimRestoreRequestSchema {
		return empty, errors.New("Claim history restore request schema is invalid")
	}
	if err := errors.Join(request.Policy.validate(request.Expected), request.Original.validate()); err != nil {
		return empty, err
	}
	_, declared, err := monitorHistoryRestoreDeclaration(ctx)
	if err != nil {
		return empty, err
	}
	root, err := newMonitorHistoryRestoreRootReview(ctx, request.Preparation, declared)
	if err != nil {
		return empty, err
	}
	if err := validateMonitorClaimRestoreHistory(ctx, request.Policy, request.Original, func(reference monitorHistoryReference) ([]byte, error) {
		return root.read(ctx, reference)
	}); err != nil {
		return empty, err
	}
	if err := root.finish(ctx); err != nil {
		return empty, err
	}
	return root.request, ctx.Err()
}

// Reuse actual owner admission, including every unpaid epoch, original
// receipt/proof witness and policy/catalog predecessor. Restoring byte custody
// cannot supply new expected epochs or make an asserted payment authoritative.
func validateMonitorClaimRestoreHistory(ctx context.Context, policy monitorClaimPolicy, original monitorHistoryReference, read func(monitorHistoryReference) ([]byte, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := read(original)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := decodeMonitorClaimCheckpoint(raw, policy)
	if err != nil {
		return err
	}
	canonical, err := encodeMonitorClaimCheckpoint(record)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, raw) {
		return errors.New("Claim restore requires the exact original checkpoint frame")
	}
	if err := record.Catalog.checkPath(original.Path); err != nil {
		return err
	}
	if record.Archive != nil {
		for _, reference := range record.Archive.Segments {
			if monitorHistoryPathsAlias(reference.Path, original.Path) {
				return errors.New("Claim restore checkpoint aliases original archive custody")
			}
		}
	}
	basis, _, _, err := replayMonitorClaimHistory(ctx, policy, record, func(reference monitorHistoryReference) ([]byte, error) {
		if monitorHistoryPathsAlias(reference.Path, original.Path) {
			return nil, errors.New("Claim restore window aliases current checkpoint custody")
		}
		return read(reference)
	})
	if err != nil {
		return err
	}
	if _, err := hydrateMonitorClaimRecord(record, basis, policy); err != nil {
		return err
	}
	return ctx.Err()
}

func runMonitorClaimArchiveRestoreRequest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("monitor-claim-archive restore-request", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "exact retained Claim history and target request")
	hash := flags.String("request-sha256", "", "accepted request digest")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" || *hash == "" {
		fmt.Fprintln(stderr, "Claim restore requires an exact request and digest")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "Claim restore input read:", err)
		return 2
	}
	if digest != *hash {
		fmt.Fprintln(stderr, "Claim restore input differs from reviewed digest")
		return 2
	}
	var request monitorClaimRestoreRequest
	if err = decodeMonitorHistoryInput(raw, &request); err == nil {
		var result durablevolume.PreparationRequest
		result, err = buildMonitorClaimRestoreRequest(ctx, request)
		if err == nil {
			raw, err = json.Marshal(result)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "Claim restore:", err)
		return 2
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "Claim restore request not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
