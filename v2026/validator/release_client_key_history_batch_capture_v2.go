//go:build linux || darwin

// A finite live census owns every existing immutable slot before transport.
// The namespace lock spans original reads, batched observations and readback.
package validator

import (
	"context"
	"crypto/rand"
	"errors"
	"reflect"
	"slices"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"golang.org/x/sys/unix"
)

// Each response keeps its independent registration and full capture identity.
type releaseClientKeyBatchV2Capture struct {
	registration protocol.ClientKeyRegistration
	contentHash  string
}

// Missing slots alone request fresh observations. A retained slot supplies its
// original nonce and bytes, never a request that asks the server to backdate.
func captureReleaseClientKeysV2(ctx context.Context, chain *ChainClient, reader *HTTPClientKeyHistoryReader, stateDir string, domain protocol.ClientKeyHistoryDomain, requests []protocol.ClientKeyObservationRequest, maximum, maximumHistory uint64) (result []releaseClientKeyBatchV2Capture, resultErr error) {
	if ctx == nil || reader == nil || maximum == 0 || maximum > protocol.MaxClientKeyHistoryResponseBytes || maximumHistory == 0 || len(requests) == 0 || len(requests) > protocol.MaxClientKeyObservationBatchClients {
		return nil, errors.New("client-key batch capture owner or bound is incomplete")
	}
	reads, ok := ctx.Value(releaseClientKeyAuthorityV2ContextKey{}).(*releaseClientKeyAuthorityV2Reads)
	if !ok || reads == nil {
		return nil, errors.New("client-key batch capture has no independently pinned authority owner")
	}
	if err := reads.validate(ctx, chain, domain); err != nil {
		return nil, err
	}
	requests = slices.Clone(requests)
	for index := range requests {
		requests[index].Nonce = [32]byte{1}
	}
	if err := (protocol.ClientKeyObservationBatchRequest{Requests: requests, MaximumResponseBytes: maximum}).Validate(); err != nil {
		return nil, err
	}
	if err := func() error {
		reads.stateLock.Lock()
		defer reads.stateLock.Unlock()
		return reads.budget.charge(uint64(len(requests)), uint64(reflect.TypeFor[releaseMeasurementInputV2Owner]().Size()+reflect.TypeFor[releaseClientKeyCapturedV2Response]().Size()+reflect.TypeFor[releaseClientKeyBatchV2Capture]().Size()+reflect.TypeFor[protocol.ClientKeyObservationRequest]().Size())+512)
	}(); err != nil {
		return nil, err
	}
	maximum, err := releaseClientKeyCaptureV2Maximum(ctx, maximum)
	if err != nil {
		return nil, err
	}
	owners := make([]*releaseMeasurementInputV2Owner, len(requests))
	sources := make([]releaseClientKeyCapturedV2Response, len(requests))
	retained := make([]bool, len(requests))
	missing := make([]int, 0, len(requests))
	var historyBytes, historyEntries, responseBytes uint64
	defer func() {
		for index := len(owners) - 1; index >= 0; index-- {
			resultErr = errors.Join(resultErr, owners[index].finish())
		}
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	for index, request := range requests {
		if responseBytes >= maximum {
			return nil, errors.New("client-key batch retained census exhausted the complete response allowance")
		}
		path, err := releaseClientKeyCaptureV2Path(stateDir, domain, request)
		if err != nil {
			return nil, err
		}
		owner, err := acquireReleaseMeasurementInputV2Owner(ctx, path, maximum-responseBytes, releaseMeasurementInputV2ReadHooks{}, true)
		owners[index] = owner
		if err != nil {
			return nil, err
		}
		if index == 0 {
			if err := unix.Flock(int(owner.directory.file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
				return nil, errors.Join(errors.New("client-key batch capture namespace is busy"), err)
			}
			historyBytes, historyEntries, err = censusReleaseEvidenceCapturesV2(ctx, owner.directory, maximumHistory, reads.maximumCaptureFiles)
			if err != nil {
				return nil, err
			}
		} else if owner.directory.anchor.dev != owners[0].directory.anchor.dev || owner.directory.anchor.ino != owners[0].directory.anchor.ino {
			return nil, errors.New("client-key batch capture escaped its locked directory identity")
		}
		encoded, err := owner.read()
		retained[index] = err == nil
		if err != nil {
			if !owner.initialMissing || !releaseMeasurementInputV2OnlyMissing(err) {
				return nil, err
			}
			if _, err := rand.Read(request.Nonce[:]); err != nil {
				return nil, err
			}
			requests[index] = request
			missing = append(missing, index)
		} else {
			responseBytes += uint64(len(encoded))
		}
		sources[index] = releaseClientKeyCapturedV2Response{encoded: encoded, maximum: maximum, domain: domain, request: request}
	}
	if len(missing) != 0 {
		if historyEntries > reads.maximumCaptureFiles || uint64(len(missing)) > reads.maximumCaptureFiles-historyEntries || historyBytes >= maximumHistory || responseBytes >= maximum {
			return nil, errors.New("client-key batch capture reached its finite history allowance")
		}
		fresh := make([]protocol.ClientKeyObservationRequest, len(missing))
		for index, position := range missing {
			fresh[index] = requests[position]
			if err := owners[position].check(); err != nil {
				return nil, err
			}
		}
		responses, err := reader.ReadBatch(ctx, fresh, min(maximum-responseBytes, maximumHistory-historyBytes))
		if err != nil {
			return nil, err
		}
		for index, encoded := range responses {
			if uint64(len(encoded)) > maximum-responseBytes || uint64(len(encoded)) > maximumHistory-historyBytes {
				return nil, errors.New("client-key batch fresh response escaped the admitted history/control allowance")
			}
			responseBytes += uint64(len(encoded))
			historyBytes += uint64(len(encoded))
			sources[missing[index]].encoded = encoded
		}
	}
	queries := []stabi.ClientKeyAuthorityQuery{}
	queryKVs := make(map[stabi.ClientKeyAuthorityQuery]bool)
	for index, source := range sources {
		if err := reads.reserveResponse(ctx, chain, domain, source.request, uint64(len(source.encoded))); err != nil {
			return nil, err
		}
		members, err := releaseClientKeyBatchV2Queries(ctx, source, retained[index])
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if !queryKVs[member] {
				queries = append(queries, member)
				queryKVs[member] = true
			}
		}
	}
	if err := reads.prefetch(ctx, chain, queries); err != nil {
		return nil, err
	}
	result = make([]releaseClientKeyBatchV2Capture, len(sources))
	for index, source := range sources {
		registration, err := verifyReservedReleaseClientKeyCaptureV2(ctx, chain, source.encoded, source.maximum, domain, source.request, retained[index])
		if err != nil {
			return nil, err
		}
		result[index] = releaseClientKeyBatchV2Capture{registration: registration, contentHash: ReleaseMeasurementContentHash(source.encoded)}
	}
	// Refuse the whole in-memory result before writing if any sibling failed.
	// A later storage failure preserves already-created exact immutable slots.
	for index, source := range sources {
		var err error
		if retained[index] {
			err = owners[index].sync()
		} else {
			err = owners[index].write(source.encoded)
		}
		if err := errors.Join(err, owners[index].check()); err != nil {
			return nil, err
		}
	}
	return result, nil
}
