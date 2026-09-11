//go:build linux || darwin

package validator

// Replicated sealing joins two independently configured object publishers and
// verifies their public HTTP copies. It does not activate a producer, decide
// the required validator census, or submit an on-chain evidence commitment.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Each publisher must own a distinct immutable storage namespace behind its
// trusted public origin. Callbacks own their byte slices, honor cancellation,
// and return only after their storage work has joined. They receive no key.
// Shared configurations require concurrency-safe callbacks. Callbacks may read
// Head or append, but must not close the source ledger or enter any operation
// that starts a nested Walk: the sealer owns its fixed-prefix walk throughout
// the callback, including when the callback runs on a publisher worker.
type AttemptCutV2Replica struct {
	Origin        string
	WriteRecords  AttemptStreamV2ObjectWriter
	WriteProofs   AttemptStreamV2ObjectWriter
	WriteMetadata AttemptStreamV2ObjectWriter
}

// The caller authenticates the independent origins and their storage ownership
// through deployment configuration. No stream, disk or timeout cap is raised.
type AttemptCutV2ReplicaOptions struct {
	ReplayBounds     AttemptCutV2ReplayBounds
	ScratchDirectory string
	ServerKeys       map[byte]ed25519.PublicKey
	Replicas         [2]AttemptCutV2Replica
}

// A successful result binds the exact signed header bytes and both queried
// origins. Partial immutable writes remain staged on error, but no publication
// result escapes. Availability here is observed, not guaranteed indefinitely.
type AttemptCutV2Publication struct {
	Cut         *AttemptCutV2
	Replay      AttemptCutV2ReplayResult
	ContentHash string
	Size        uint64
	Origins     [2]string
}

// Seals through the real complete replay, then publishes the canonical signed
// header to both replicas. The VPK stays local; operator storage callbacks do
// not sign validator evidence or replace record/proof authentication.
// Inputs must remain unchanged for this invocation. The caller retains all
// scratch/staged objects and the sealer's cut/drain ownership obligations.
func SealReplicatedAttemptCutV2(ctx context.Context, ledger *AttemptLedger, expected AttemptCutV2Context, policy protocol.Policy, privateKey ed25519.PrivateKey, bounds AttemptCutV2Bounds, options AttemptCutV2ReplicaOptions) (publication *AttemptCutV2Publication, resultErr error) {
	if ctx == nil {
		return nil, errors.New("replicated attempt cut context is missing")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			publication = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	replicas, err := newAttemptCutV2Replicas(bounds, options.Replicas)
	if err != nil {
		return nil, err
	}
	cut, replay, err := SealAttemptCutV2(ctx, ledger, expected, policy, privateKey, bounds, AttemptCutV2SealOptions{
		ReplayBounds: options.ReplayBounds, ScratchDirectory: options.ScratchDirectory, ServerKeys: options.ServerKeys,
		WriteRecords: replicas.writer(AttemptStreamV2Records), WriteProofs: replicas.writer(AttemptStreamV2Proofs),
		WriteMetadata: replicas.writer("metadata"), ReadMetadata: replicas.readers[0].ReadMetadata, OpenData: replicas.readers[0].OpenData,
	})
	if err != nil {
		return nil, err
	}
	raw, err := cut.CanonicalJSON(bounds)
	if err != nil {
		return nil, err
	}
	contentHash := attemptHex32(sha256.Sum256(raw))
	if err := replicas.writer("metadata")(ctx, contentHash, raw); err != nil {
		return nil, err
	}
	return &AttemptCutV2Publication{
		Cut: cut, Replay: replay, ContentHash: contentHash, Size: uint64(len(raw)),
		Origins: [2]string{options.Replicas[0].Origin, options.Replicas[1].Origin},
	}, nil
}

// Immutable configuration can serve independent cuts concurrently. Every
// object operation owns its two bounded workers and all of their responses.
type attemptCutV2Replicas struct {
	replicas [2]AttemptCutV2Replica
	readers  [2]*HTTPAttemptStreamV2Reader
}

// Resolve all origins, callbacks and bounds before invoking either publisher.
// DNS aliases cannot establish independent ownership; deployment admission is
// still responsible for that. Obvious same-origin aliases are rejected here.
func newAttemptCutV2Replicas(bounds AttemptCutV2Bounds, replicas [2]AttemptCutV2Replica) (*attemptCutV2Replicas, error) {
	return newAttemptCutV2ReplicasWithMetadataLimit(bounds, replicas, attemptStreamV2MetadataBytes(bounds))
}

// Terminal evidence uses its admitted transition allowance without changing
// the stream schemas or the default cut publisher's metadata capacity.
func newAttemptCutV2ReplicasWithMetadataLimit(bounds AttemptCutV2Bounds, replicas [2]AttemptCutV2Replica, metadataBytes uint64) (*attemptCutV2Replicas, error) {
	self := &attemptCutV2Replicas{replicas: replicas}
	var origins [2]string
	for index, replica := range replicas {
		if replica.WriteRecords == nil || replica.WriteProofs == nil || replica.WriteMetadata == nil {
			return nil, errors.New("replicated attempt publisher is incomplete")
		}
		reader, err := newHttpAttemptStreamV2Reader(replica.Origin, bounds, metadataBytes)
		if err != nil {
			return nil, err
		}
		if bounds.MaxHeaderBytes == 0 || bounds.MaxHeaderBytes > min(attemptStreamV2MetadataBytes(bounds), reader.metadataBytes) {
			return nil, errors.New("replicated attempt header exceeds its public metadata bound")
		}
		endpoint, err := url.Parse(replica.Origin)
		if err != nil {
			return nil, err
		}
		host := strings.ToLower(endpoint.Hostname())
		if ip := net.ParseIP(host); ip != nil {
			host = ip.String()
		}
		port := endpoint.Port()
		if port == "" {
			port = "443"
			if endpoint.Scheme == "http" {
				port = "80"
			}
		}
		origins[index] = endpoint.Scheme + "://" + net.JoinHostPort(host, port)
		self.readers[index] = reader
	}
	if origins[0] == origins[1] {
		return nil, errors.New("replicated attempt origins are not distinct")
	}
	return self, nil
}

// No callback sees another callback's mutable bytes. This operation owns two
// object copies, fixed data-read buffers, and at most two bounded metadata
// readback bodies. No buffer or retained result grows with the stream history.
// Any failure cancels siblings, but cancellation never substitutes for joining.
func (self *attemptCutV2Replicas) writer(kind string) AttemptStreamV2ObjectWriter {
	return func(ctx context.Context, contentHash string, raw []byte) error {
		if ctx == nil {
			return errors.New("replicated attempt object context is missing")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		expected, err := canonicalAttemptHex32("replicated attempt object hash", contentHash, false)
		if err != nil {
			return err
		}
		var limit uint64
		switch kind {
		case "metadata":
			limit = self.readers[0].metadataBytes
		case AttemptStreamV2Records:
			limit = self.readers[0].recordBytes
		case AttemptStreamV2Proofs:
			limit = self.readers[0].proofBytes
		default:
			return errors.New("replicated attempt object kind is unsupported")
		}
		if len(raw) == 0 || uint64(len(raw)) > limit || sha256.Sum256(raw) != expected {
			return errors.New("replicated attempt object differs from its typed bound or hash")
		}
		size := uint64(len(raw))
		ownedCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		var workers sync.WaitGroup
		var results [2]error
		for index, replica := range self.replicas {
			write := replica.WriteMetadata
			if kind == AttemptStreamV2Records {
				write = replica.WriteRecords
			} else if kind == AttemptStreamV2Proofs {
				write = replica.WriteProofs
			}
			// The original remains borrowed until both workers join. Callbacks
			// own independent copies and may mutate them after returning.
			data := append([]byte(nil), raw...)
			workers.Add(1)
			go func(index int, write AttemptStreamV2ObjectWriter, data []byte) {
				// Goexit runs defers without returning from a callback or reader.
				// Keep failure until all required work returns, and publish it
				// before Done so joining cannot manufacture an acknowledgment.
				outcome := errors.New("attempt replica worker did not complete publication")
				defer func() {
					if err := errors.Join(outcome, ownedCtx.Err()); err != nil {
						results[index] = fmt.Errorf("attempt replica %d: %w", index+1, err)
						cancel()
					}
					workers.Done()
				}()
				if err := ownedCtx.Err(); err != nil {
					outcome = err
					return
				}
				if err := write(ownedCtx, contentHash, data); err != nil {
					outcome = err
					return
				}
				outcome = self.verify(ownedCtx, index, kind, contentHash, size)
			}(index, write, data)
		}
		workers.Wait()
		return errors.Join(results[0], results[1], ctx.Err())
	}
}

// Public retrieval is mandatory even after a successful local storage write.
// The reader enforces media type, exact length/hash, EOF, Close and cancellation.
func (self *attemptCutV2Replicas) verify(ctx context.Context, index int, kind, contentHash string, size uint64) (resultErr error) {
	if kind == "metadata" {
		_, err := self.readers[index].ReadMetadata(ctx, contentHash, size)
		return err
	}
	reader, err := self.readers[index].OpenData(ctx, kind, contentHash, size)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, reader.Close(), ctx.Err()) }()
	_, resultErr = io.CopyBuffer(io.Discard, reader, make([]byte, 32*1024))
	return resultErr
}
