//go:build linux || darwin

// Restore request construction authenticates the whole retained native history
// before deriving its fixed snapshot owner census. Publication still requires
// the separate reviewed storage-prepare plan/apply; this command is read-only.
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

const monitorNativeRestoreRequestSchema = "urnetwork-native-monitor-restore-request-v1"

// Preparation retains all independent target choices and additional co-owners.
// Native snapshots are derived from original references, never a filename glob.
type monitorNativeRestoreRequest struct {
	Schema      string                           `json:"schema"`
	Expected    identityExpectation              `json:"expected"`
	Policy      monitorEconomicNativePolicy      `json:"policy"`
	Original    monitorHistoryReference          `json:"original"`
	Preparation durablevolume.PreparationRequest `json:"preparation"`
}

// The original logical root is retained: capacity approvals and segment paths
// cannot be silently rewritten to fit a different deployment namespace.
func buildMonitorNativeRestoreRequest(ctx context.Context, request monitorNativeRestoreRequest) (result durablevolume.PreparationRequest, resultErr error) {
	if err := durablepath.Require(ctx); err != nil {
		return result, err
	}
	if request.Schema != monitorNativeRestoreRequestSchema || request.Preparation.Purpose != "restore" || request.Preparation.Scope != "daemon" || request.Preparation.RestoreSource == nil {
		return result, errors.New("native history restore requires an explicit daemon restore request")
	}
	if err := errors.Join(request.Policy.validate(request.Expected), request.Original.validate()); err != nil {
		return result, err
	}
	_, declared, err := monitorHistoryRestoreDeclaration(ctx)
	if err != nil {
		return result, err
	}
	root, err := newMonitorHistoryRestoreRootReview(ctx, request.Preparation, declared)
	if err != nil {
		return result, err
	}
	// This single-root command restores monitor history. Its existing external
	// producer inputs remain exact reads; originals inside this root use copies.
	ctx = context.WithValue(ctx, nativeProducerRuntimeReadKey{}, func(ctx context.Context, reference planFileReference, maximum int) ([]byte, error) {
		raw, _, err := readNativeProducerRestoreOriginal(ctx, reference, maximum, []*monitorHistoryRestoreRootReview{root}, nil)
		return raw, err
	})
	if err := validateMonitorNativeRestoreHistory(request.Policy, request.Original, func(reference monitorHistoryReference) ([]byte, error) {
		return root.read(ctx, reference)
	}, ctx); err != nil {
		return result, err
	}
	if err := root.finish(ctx); err != nil {
		return result, err
	}
	return root.request, ctx.Err()
}

// Each source inventory can live under a different root. The original signed
// paths and exact predecessor summaries, not root placement, bind the history.
func validateMonitorNativeRestoreHistory(policy monitorEconomicNativePolicy, original monitorHistoryReference, read func(monitorHistoryReference) ([]byte, error), contexts ...context.Context) error {
	ctx := nativeRuntimeAdmissionContext(contexts)
	raw, err := read(original)
	if err != nil {
		return err
	}
	record, err := decodeMonitorEconomicNativeCheckpoint(raw, policy, ctx)
	if err != nil {
		return err
	}
	canonical, err := encodeMonitorNativeCheckpoint(record)
	if err != nil || !bytes.Equal(canonical, raw) {
		return errors.Join(errors.New("native restore requires the exact original checkpoint frame"), err)
	}
	if err := record.State.Catalog.checkPath(original.Path); err != nil {
		return err
	}
	var prior *monitorEconomicNativeArchive
	if record.State.Archive != nil {
		for _, reference := range record.State.Archive.Segments {
			raw, err := read(reference)
			if err != nil {
				return err
			}
			segment, err := decodeMonitorEconomicNativeCheckpoint(raw, policy, ctx)
			if err != nil {
				return err
			}
			if !record.State.Catalog.retains(segment.State.Catalog) || !reflect.DeepEqual(segment.State.Archive, prior) {
				return errors.New("native restore discarded a signed revision or an original archive predecessor")
			}
			next, err := compactMonitorEconomicNative(segment, reference, policy)
			if err != nil {
				return err
			}
			prior = next.State.Archive
		}
	}
	if !reflect.DeepEqual(prior, record.State.Archive) {
		return errors.New("native restore summary differs from complete original history")
	}
	return nil
}

// This emits a strict storage-prepare request for independent review. It does
// not create any target, change an approval, sign, reconcile or enable a service.
func runMonitorNativeArchiveRestoreRequest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("monitor-native-archive restore-request", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "exact retained native history and target request")
	hash := flags.String("request-sha256", "", "accepted request digest")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" || *hash == "" {
		fmt.Fprintln(stderr, "native restore requires an exact request and digest")
		return 2
	}
	raw, digest, err := readPlanFile(ctx, *path, maxRpcReplyBytes)
	if err != nil {
		fmt.Fprintln(stderr, "native restore input read:", err)
		return 2
	}
	if digest != *hash {
		fmt.Fprintln(stderr, "native restore input differs from reviewed digest")
		return 2
	}
	var request monitorNativeRestoreRequest
	if err = decodeMonitorHistoryInput(raw, &request); err == nil {
		var result durablevolume.PreparationRequest
		result, err = buildMonitorNativeRestoreRequest(ctx, request)
		if err == nil {
			raw, err = json.Marshal(result)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "native restore:", err)
		return 2
	}
	raw = append(raw, '\n')
	n, err := stdout.Write(raw)
	if err != nil || n != len(raw) {
		fmt.Fprintln(stderr, "native restore request not delivered:", errors.Join(io.ErrShortWrite, err))
		return 2
	}
	return 0
}
