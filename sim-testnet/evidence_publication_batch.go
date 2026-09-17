// A bounded publication scope carries only the live local quota leases.
// Immutable envelope authentication and every replica readback remain in the
// ordinary publisher; no signed payload is retained between accept calls.
package main

import (
	"context"
	"errors"

	"github.com/urnetwork/server/v2026"
)

// Sixty-four envelopes bound lock tenure independently of the archive census.
// Each envelope still writes two immutable routes at every configured store.
const campaignEvidenceBatchEnvelopes = 64

// Close each physical-root owner before beginning another finite batch, and
// join final-accounting errors before the caller may publish an archive result.
// The body and its publication calls are synchronous and may not escape.
func withCampaignEvidencePublicationBatches(ctx context.Context, stores map[int]server.BlobStore, body func(func(*ReleaseEvidenceEnvelope) error) error) (resultErr error) {
	if ctx == nil || len(stores) == 0 || len(stores) > 128 || body == nil {
		return errors.New("campaign evidence batch ownership is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ordered := make([]server.BlobStore, len(stores))
	for operator := 1; operator <= len(stores); operator++ {
		if stores[operator] == nil {
			return errors.New("campaign evidence batch has a missing operator store")
		}
		ordered[operator-1] = stores[operator]
	}
	var batch *server.LocalBlobWriteBatch
	closed := false
	defer func() {
		closed = true
		resultErr = errors.Join(resultErr, batch.Close(), ctx.Err())
	}()
	batchContext := ctx
	accepted := 0
	var publicationErr error
	publish := func(envelope *ReleaseEvidenceEnvelope) (resultErr error) {
		if closed {
			return errors.New("campaign evidence publication scope is closed")
		}
		if publicationErr != nil {
			return publicationErr
		}
		defer func() { publicationErr = errors.Join(publicationErr, resultErr) }()
		if err := ctx.Err(); err != nil {
			return err
		}
		if accepted%campaignEvidenceBatchEnvelopes == 0 {
			if err := batch.Close(); err != nil {
				return err
			}
			batch = nil
			batchContext = ctx
			var err error
			batch, err = server.BeginLocalBlobWriteBatch(ctx, ordered, 2*len(ordered)*campaignEvidenceBatchEnvelopes)
			if err != nil {
				return err
			}
			if batch != nil {
				batchContext = batch.Context()
			}
		}
		if err := publishCampaignEvidenceReplicas(batchContext, stores, envelope); err != nil {
			return err
		}
		accepted++
		return nil
	}
	bodyErr := body(publish)
	return errors.Join(bodyErr, publicationErr)
}
