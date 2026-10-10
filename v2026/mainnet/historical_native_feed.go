// A native capture can request missing parent-trie nodes through two owned
// pipes. Only the Go owner chooses the archive endpoint and durable cache.
// Every returned proof remains candidate bytes until the SDK traverses the
// exact certified root; no RPC value, page boundary or claimed root is trusted.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/urnetwork/server/v2026/strecovery"
	"golang.org/x/crypto/blake2b"
)

const historicalNativeFeedSchema = "urnetwork-native-parent-trie-refill-v1"
const historicalNativeFeedFrame = 16 * 1024

type historicalNativeFeedRequest struct {
	Schema          string `json:"schema"`
	Id              uint64 `json:"id"`
	RequestSha256   string `json:"request_sha256"`
	ParentHash      string `json:"parent_hash"`
	ParentStateRoot string `json:"parent_state_root"`
	Operation       string `json:"operation"`
	ChildStorageKey string `json:"child_storage_key"`
	PrefixHex       string `json:"prefix_hex"`
	PrefixNibble    *uint8 `json:"prefix_nibble"`
	MissingHash     string `json:"missing_hash"`
}

type historicalNativeFeedResponse struct {
	Schema        string `json:"schema"`
	Id            uint64 `json:"id"`
	RequestSha256 string `json:"request_sha256"`
	MissingHash   string `json:"missing_hash"`
	Retained      bool   `json:"retained"`
}

// One feed belongs to one immutable capture input and is used serially. Nodes
// retained by earlier jobs remain immutable; no missing completed job is healed.
type historicalNativeFeed struct {
	files           *nativeProducerFiles
	client          *rpcClient
	requestSha256   string
	parentHash      string
	parentStateRoot string
}

func (self *historicalNativeFeed) validate(input historicalCaptureInput, raw []byte, nodes string) error {
	if self == nil || self.files == nil || self.client == nil || !historicalNativeProfile(input.ObservationProfile) || self.requestSha256 != monitorReadDigest(raw) || self.parentHash != "0x"+hex.EncodeToString(input.ParentHash[:]) || !rootCanonicalHash(self.parentStateRoot) || nodes != filepath.Join(self.files.path, "nodes") {
		return errors.Join(errRpcIntegrity, errors.New("native proof feed differs from its original capture and owned cache"))
	}
	return self.files.check()
}

// Framing cannot choose a path, route, RPC method or child after a response.
// A padded odd nibble forms a probe below the exact missing-node prefix. SDK
// traversal still establishes absence and completeness after the node arrives.
func (self *historicalNativeFeed) probe(request historicalNativeFeedRequest, sequence uint64) ([]byte, []byte, error) {
	if request.Schema != historicalNativeFeedSchema || request.Id != sequence || request.RequestSha256 != self.requestSha256 || request.ParentHash != self.parentHash || request.ParentStateRoot != self.parentStateRoot || !rootCanonicalHash(request.MissingHash) {
		return nil, nil, errors.Join(errRpcIntegrity, errors.New("native proof feed request identity differs"))
	}
	child, err := historicalReplayHex(request.ChildStorageKey, 1024)
	if err != nil {
		return nil, nil, errors.Join(errRpcIntegrity, err)
	}
	key, err := historicalReplayHex(request.PrefixHex, 1024)
	if err != nil || request.PrefixNibble != nil && (*request.PrefixNibble&15 != 0 || len(key) >= 1024) {
		return nil, nil, errors.Join(errRpcIntegrity, errors.New("native proof feed prefix is not canonical"), err)
	}
	childOperation := strings.HasPrefix(request.Operation, "child-") && request.Operation != "child-root"
	switch request.Operation {
	case "storage", "storage-hash", "closest-merkle", "next-key", "storage-root", "child-root":
		if len(child) != 0 {
			return nil, nil, errors.Join(errRpcIntegrity, errors.New("native top-level proof requested a child namespace"))
		}
	case "child-storage", "child-storage-hash", "child-closest-merkle", "child-next-key", "child-storage-root", "iterator-open", "iterator-key", "iterator-pair":
	default:
		return nil, nil, errors.Join(errRpcIntegrity, errors.New("native proof feed operation is outside its read profile"))
	}
	if childOperation && len(child) == 0 || len(child) != 0 && (!strings.HasPrefix(string(child), ":child_storage:default:") || len(child) == len(":child_storage:default:")) {
		return nil, nil, errors.Join(errRpcIntegrity, errors.New("native proof feed child scope is not exact"))
	}
	if request.PrefixNibble != nil {
		key = append(key, *request.PrefixNibble)
	}
	return child, key, nil
}

// One shared reader counts every retry/reply. Missing requested nodes never
// publish unrelated candidates or extend this capture's original deadline.
func (self *historicalNativeFeed) serve(ctx context.Context, input io.Reader, output io.Writer) error {
	reader, err := strecovery.NewNativeExecutionProofReader(self.client.url, self.client.retryWindow)
	if err != nil {
		return err
	}
	defer reader.Close()
	seen := map[string]int{}
	var bytes int
	for sequence := uint64(1); sequence <= 2*historicalNativeProofNodes; sequence++ {
		if err := errors.Join(ctx.Err(), self.files.check()); err != nil {
			return err
		}
		var length [4]byte
		n, err := io.ReadFull(input, length[:])
		if n == 0 && err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.Join(err, ctx.Err())
		}
		size := binary.BigEndian.Uint32(length[:])
		if size == 0 || size > historicalNativeFeedFrame {
			return errors.Join(errRpcIntegrity, errors.New("native proof feed request exceeds its frame"))
		}
		raw := make([]byte, int(size))
		if _, err := io.ReadFull(input, raw); err != nil {
			return errors.Join(err, ctx.Err())
		}
		var request historicalNativeFeedRequest
		if err := decodePlanJson(raw, &request); err != nil {
			return errors.Join(errRpcIntegrity, err)
		}
		child, key, err := self.probe(request, sequence)
		if err != nil {
			return err
		}
		if _, already := seen[request.MissingHash]; already {
			return errors.Join(errRpcIntegrity, errors.New("native proof feed lost a node already acknowledged in this capture"))
		}
		for {
			proof, err := reader.Read(ctx, self.parentHash, child, key)
			if err != nil {
				// Retain the refused attempt before the supervisor cancels the
				// child. A bounded prefix is explicitly diagnostic, never proof.
				// An already-ended owner cannot write; its returned error still
				// carries the method, identity, stage and observed-reply digest.
				var failure *strecovery.NativeExecutionProofReadFailure
				if errors.As(err, &failure) {
					raw, encodeErr := json.Marshal(failure)
					if encodeErr == nil {
						encodeErr = self.files.admitMargin(1)
					}
					if encodeErr == nil {
						_, encodeErr = self.files.publish("proof-rpc/"+strings.TrimPrefix(monitorReadDigest(raw), "sha256:")+".json", raw, 128*1024)
					}
					err = errors.Join(err, encodeErr)
				}
				if errors.Is(err, strecovery.ErrNativeExecutionProofConflict) {
					err = errors.Join(errRpcIntegrity, err)
				}
				return err
			}
			nodes := map[string][]byte{}
			var newBytes int
			for _, encoded := range proof.Proof {
				raw, err := historicalReplayHex(encoded, historicalNativeProofNodeBytes)
				if err != nil || len(raw) == 0 {
					return errors.Join(errRpcIntegrity, errors.New("native proof feed returned a malformed raw node"), err)
				}
				digest := blake2b.Sum256(raw)
				hash := "0x" + hex.EncodeToString(digest[:])
				if _, duplicate := nodes[hash]; duplicate {
					continue
				}
				nodes[hash] = raw
				if _, retained := seen[hash]; !retained {
					newBytes += len(raw)
				}
			}
			if _, present := nodes[request.MissingHash]; !present {
				wait := self.client.retryWait
				if wait == nil {
					wait = waitRpcReadRetry
				}
				if err := wait(ctx, time.Second); err != nil {
					return errors.Join(errRpcObservationUnavailable, strecovery.ErrNativeStorageIncomplete, err)
				}
				continue
			}
			newCount := 0
			for hash := range nodes {
				if _, retained := seen[hash]; !retained {
					newCount++
				}
			}
			if newCount > historicalNativeProofNodes-len(seen) || newBytes > historicalNativeProofBytes-bytes {
				return errMonitorEconomicCapacity
			}
			// Preflight the full returned batch before any node mutation. One
			// initial artifact census reserved the complete native proof profile.
			hashes := make([]string, 0, len(nodes))
			for hash := range nodes {
				hashes = append(hashes, hash)
			}
			slices.Sort(hashes)
			for _, hash := range hashes {
				if err := ctx.Err(); err != nil {
					return err
				}
				if _, err := self.files.publish("nodes/"+strings.TrimPrefix(hash, "0x"), nodes[hash], historicalNativeProofNodeBytes); err != nil {
					return err
				}
				if _, retained := seen[hash]; !retained {
					seen[hash] = len(nodes[hash])
					bytes += len(nodes[hash])
				}
			}
			break
		}
		response := historicalNativeFeedResponse{Schema: historicalNativeFeedSchema, Id: sequence, RequestSha256: self.requestSha256, MissingHash: request.MissingHash, Retained: true}
		raw, err = json.Marshal(response)
		if err != nil {
			return err
		}
		binary.BigEndian.PutUint32(length[:], uint32(len(raw)))
		if err := historicalNativeFeedWrite(output, length[:]); err != nil {
			return err
		}
		if err := historicalNativeFeedWrite(output, raw); err != nil {
			return err
		}
	}
	return errMonitorEconomicCapacity
}

func historicalNativeFeedWrite(writer io.Writer, raw []byte) error {
	n, err := writer.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	return err
}

// Parent and child pipe copies have separate single-close owners. Cancellation
// closes parent I/O to join blocked frame reads/writes; the supervisor still
// kills and reaps the entire original process group before report acceptance.
type historicalNativeFeedPipes struct {
	requestRead, requestWrite, responseRead, responseWrite *os.File
	parentOnce, childOnce                                  sync.Once
	parentErr, childErr                                    error
}

func newHistoricalNativeFeedPipes() (*historicalNativeFeedPipes, error) {
	requestRead, requestWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		return nil, errors.Join(err, requestRead.Close(), requestWrite.Close())
	}
	return &historicalNativeFeedPipes{requestRead: requestRead, requestWrite: requestWrite, responseRead: responseRead, responseWrite: responseWrite}, nil
}

func (self *historicalNativeFeedPipes) closeParent() error {
	self.parentOnce.Do(func() { self.parentErr = errors.Join(self.requestRead.Close(), self.responseWrite.Close()) })
	return self.parentErr
}

func (self *historicalNativeFeedPipes) closeChild() error {
	self.childOnce.Do(func() { self.childErr = errors.Join(self.requestWrite.Close(), self.responseRead.Close()) })
	return self.childErr
}

// Preserve the actual broker cause even when it cancels the owned child. In
// particular an unavailable proof must not become a fabricated root conflict.
func historicalNativeFeedError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("native parent trie refill: %w", err)
}
