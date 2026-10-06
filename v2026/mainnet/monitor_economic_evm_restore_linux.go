//go:build linux

// Restore selects exact original snapshot owners after checking the complete
// retained EVM history. Target publication remains a separate reviewed action.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"reflect"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

const monitorEvmRestoreRequestSchema = "urnetwork-evm-monitor-restore-request-v1"

type monitorEvmRestoreRequest struct {
	Schema      string                           `json:"schema"`
	Expected    identityExpectation              `json:"expected"`
	Policy      monitorEconomicEvmPolicy         `json:"policy"`
	Original    monitorHistoryReference          `json:"original"`
	Preparation durablevolume.PreparationRequest `json:"preparation"`
}

// Logical paths, independent co-owners and original approvals remain unchanged.
// The result authorizes neither signing nor automatic service restart.
func buildMonitorEvmRestoreRequest(ctx context.Context, request monitorEvmRestoreRequest) (durablevolume.PreparationRequest, error) {
	var empty durablevolume.PreparationRequest
	if err := durablepath.Require(ctx); err != nil {
		return empty, err
	}
	if request.Schema != monitorEvmRestoreRequestSchema {
		return empty, errors.New("EVM history restore request schema is invalid")
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
	if err := validateMonitorEvmRestoreHistory(request.Policy, request.Original, func(reference monitorHistoryReference) ([]byte, error) {
		return root.read(ctx, reference)
	}); err != nil {
		return empty, err
	}
	if err := root.finish(ctx); err != nil {
		return empty, err
	}
	return root.request, ctx.Err()
}

// Recompute each archive summary from its exact original checkpoint, keeping
// at most one archived payload resident. Credit, carry, fees, pending ranges
// and resource-review ancestry use the same grammar as actual runtime reopen.
func validateMonitorEvmRestoreHistory(policy monitorEconomicEvmPolicy, original monitorHistoryReference, read func(monitorHistoryReference) ([]byte, error)) error {
	raw, err := read(original)
	if err != nil {
		return err
	}
	record, err := decodeMonitorEconomicEvmCheckpoint(raw, policy)
	if err != nil {
		return err
	}
	canonical, err := encodeMonitorEvmCheckpoint(record)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, raw) {
		return errors.New("EVM restore requires the exact original checkpoint frame")
	}
	if err := record.State.Catalog.checkPath(original.Path); err != nil {
		return err
	}
	validation, err := record.validationPolicy(policy)
	if err != nil {
		return err
	}
	currentReviews, err := record.retainedResourceHistory(policy, validation.resources())
	if err != nil {
		return err
	}
	var prior *monitorEconomicEvmArchive
	var priorReviews *monitorEvmResourceHistory
	if record.State.Archive != nil {
		for _, reference := range record.State.Archive.Segments {
			raw, err := read(reference)
			if err != nil {
				return err
			}
			segment, err := decodeMonitorEconomicEvmCheckpoint(raw, policy)
			if err != nil {
				return err
			}
			if !record.State.Catalog.retains(segment.State.Catalog) || !reflect.DeepEqual(segment.State.Archive, prior) {
				return errors.New("EVM restore discarded an original archive predecessor or signed catalog revision")
			}
			next, err := compactMonitorEconomicEvm(segment, reference, policy)
			if err != nil {
				return err
			}
			if !monitorEvmReviewsRetain(currentReviews, next.ResourceHistory) || priorReviews != nil && !monitorEvmReviewsRetain(next.ResourceHistory, priorReviews) {
				return errors.New("EVM restore discarded acknowledged resource review provenance")
			}
			prior, priorReviews = next.State.Archive, next.ResourceHistory
		}
	}
	if !reflect.DeepEqual(prior, record.State.Archive) {
		return errors.New("EVM restore summary differs from complete original history")
	}
	return nil
}

func runMonitorEvmArchiveRestoreRequest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("monitor-evm-archive restore-request", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "exact retained EVM history and target request")
	hash := flags.String("request-sha256", "", "accepted request digest")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" || *hash == "" {
		fmt.Fprintln(stderr, "EVM restore requires an exact request and digest")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "EVM restore input read:", err)
		return 2
	}
	if digest != *hash {
		fmt.Fprintln(stderr, "EVM restore input differs from reviewed digest")
		return 2
	}
	var request monitorEvmRestoreRequest
	if err = decodeMonitorHistoryInput(raw, &request); err == nil {
		var result durablevolume.PreparationRequest
		result, err = buildMonitorEvmRestoreRequest(ctx, request)
		if err == nil {
			raw, err = json.Marshal(result)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "EVM restore:", err)
		return 2
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "EVM restore request not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
