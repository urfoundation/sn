//go:build linux || darwin

// Original request inventory restores exposure hidden by a lost first reply.
// Every assignment/confirmation is deduplicated by its original trail position.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Counts alone never prove a missing request did not create an assignment.
type providerAttemptLookupRequest struct {
	Scope     *protocol.ProviderAttemptReceiptScope `json:"scope,omitempty"`
	ClientId  connect.Id                            `json:"client_id"`
	Message   []byte                                `json:"message"`
	Signature []byte                                `json:"signature"`
}

// Exact original or durable execution fence is required, never both.
type providerAttemptLookupResult struct {
	Original         *protocol.ProviderAttemptReceipt          `json:"original"`
	ClosedUnreceived *protocol.ProviderAttemptClosedUnreceived `json:"closed_unreceived,omitempty"`
}

// Returned rows are staged until every independently expected lane succeeds.
func (self *ProviderAttemptAuthoritySource) verifyRequests(ctx context.Context, original *ProviderAttemptOriginal, startMs, endMs uint64, live bool, budget *providerAttemptOriginalBudget) ([]VerifiedProviderAttemptRow, [32]byte, error) {
	type laneKey struct {
		hotkey [32]byte
		noId   uint64
	}
	lanes := map[laneKey]ProviderAttemptRequestLaneOriginal{}
	for _, lane := range original.Response.Requests {
		key := laneKey{hotkey: lane.Hotkey, noId: lane.NoId}
		if _, exists := lanes[key]; exists {
			return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
		}
		lanes[key] = lane
	}
	var expectedLanes uint64
	for _, validator := range original.Response.Authority.Validators {
		expectedLanes += uint64(len(validator.Operators))
	}
	if uint64(len(lanes)) < expectedLanes {
		return nil, [32]byte{}, protocol.ErrProviderAttemptsUnavailable
	}
	if uint64(len(lanes)) != expectedLanes {
		return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
	}
	responses := map[[32]byte]ProviderAttemptOriginalResponse{}
	for _, receipt := range original.Receipts {
		if _, exists := responses[receipt.RequestHash]; exists {
			return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
		}
		responses[receipt.RequestHash] = receipt
	}
	used := map[[32]byte]bool{}
	type eventKey struct {
		hotkey   [32]byte
		noId     uint64
		trail    connect.Id
		position int
	}
	type event struct {
		hop       protocol.ProviderAttemptReceiptHop
		confirmed bool
	}
	events := map[eventKey]event{}
	digest := sha256.New()
	_, _ = digest.Write([]byte("urnetwork-provider-request-complete-census-v1\x00"))
	var totalRecords uint64
	for _, validator := range original.Response.Authority.Validators {
		for _, operator := range validator.Operators {
			if err := ctx.Err(); err != nil {
				return nil, [32]byte{}, err
			}
			lane, exists := lanes[laneKey{hotkey: validator.Hotkey, noId: operator.Expected.Identity.NoID}]
			if !exists {
				return nil, [32]byte{}, protocol.ErrProviderAttemptsUnavailable
			}
			selected := original.Response.Authority.Window
			birth := operator.Requests.Birth
			if birth.SettlementEpoch > selected.Epoch || birth.EVMBlock > selected.StartBlock {
				return nil, [32]byte{}, protocol.ErrProviderAttemptsUnavailable
			}
			if selected.Epoch-birth.SettlementEpoch >= self.authority.MaxRecords || uint64(len(lane.Windows)) != selected.Epoch-birth.SettlementEpoch+1 {
				return nil, [32]byte{}, protocol.ErrProviderAttemptsUnavailable
			}
			keys := map[byte]ed25519.PublicKey{}
			for _, key := range operator.ServerKeys {
				keys[key.Id] = bytes.Clone(key.Key[:])
			}
			var head ProviderAttemptRequestHead
			var previousHash [32]byte
			var previousEnd uint64
			wireKVs := map[[32]byte]bool{}
			for index, cut := range lane.Windows {
				window := cut.Header.Window
				if index == 0 && birth.EVMBlock > window.StartBlock {
					return nil, [32]byte{}, protocol.ErrProviderAttemptsUnavailable
				}
				if window.Epoch != birth.SettlementEpoch+uint64(index) || index > 0 && window.StartBlock != previousEnd || window.Epoch == selected.Epoch && window != selected {
					return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
				}
				if err := VerifyProviderAttemptRequestWindow(ctx, cut, operator.Requests, head, previousHash, window, self.authority.MaxWindowBytes); err != nil {
					return nil, [32]byte{}, err
				}
				if uint64(len(cut.Records)) > self.authority.MaxRecords-totalRecords {
					return nil, [32]byte{}, protocol.ErrProviderAttemptsCapacity
				}
				totalRecords += uint64(len(cut.Records))
				cutHash, err := cut.Header.Hash()
				if err != nil {
					return nil, [32]byte{}, err
				}
				_, _ = digest.Write(cutHash[:])
				if len(cut.Closures) != 0 && len(cut.Closures) != len(cut.Records) {
					return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
				}
				for recordIndex, record := range cut.Records {
					if err := ctx.Err(); err != nil {
						return nil, [32]byte{}, err
					}
					wireHash := sha256.Sum256(append(bytes.Clone(record.Message), record.RequestSignature...))
					if wireKVs[wireHash] {
						return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
					}
					wireKVs[wireHash] = true
					requestHash, err := record.Hash()
					if err != nil {
						return nil, [32]byte{}, err
					}
					var closure *protocol.ProviderAttemptRequestClosure
					if len(cut.Closures) > 0 {
						value := cut.Closures[recordIndex]
						if value.ClientId != record.Identity.ClientId || value.CutHash != cutHash || value.Epoch != window.Epoch || value.EndBlock != window.EndBlock || !bytes.Equal(value.Message, record.Message) || !bytes.Equal(value.RequestSignature, record.RequestSignature) {
							return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
						}
						if err := protocol.VerifyProviderAttemptRequestClosure(ctx, value, operator.ReceiptScope); err != nil {
							return nil, [32]byte{}, err
						}
						closure = &value
					}
					response, exists := responses[requestHash]
					if !exists && live {
						request := providerAttemptLookupRequest{Scope: &operator.ReceiptScope, ClientId: record.Identity.ClientId, Message: record.Message, Signature: record.RequestSignature}
						raw, err := json.Marshal(request)
						if err != nil {
							return nil, [32]byte{}, err
						}
						endpoint := operator.ReceiptEndpoint
						if closure != nil {
							raw, err = json.Marshal(closure)
							if err != nil {
								return nil, [32]byte{}, err
							}
							endpoint = strings.TrimSuffix(endpoint, "/") + "/close"
						}
						body, err := providerAttemptHttp(ctx, endpoint, http.MethodPost, raw, budget.remaining(128*1024))
						if err != nil {
							return nil, [32]byte{}, err
						}
						var result providerAttemptLookupResult
						if err := decodeProviderAttemptTransport(body, &result); err != nil {
							return nil, [32]byte{}, err
						}
						response = ProviderAttemptOriginalResponse{RequestHash: requestHash, Receipt: result.Original, Closure: closure, ClosedUnreceived: result.ClosedUnreceived}
						wire, err := json.Marshal(response)
						if err != nil {
							return nil, [32]byte{}, err
						}
						if err := budget.reserve(uint64(len(wire)) + 1); err != nil {
							return nil, [32]byte{}, err
						}
						responses[requestHash] = response
						original.Receipts = append(original.Receipts, response)
					} else if !exists {
						return nil, [32]byte{}, protocol.ErrProviderAttemptsUnavailable
					}
					used[requestHash] = true
					if response.Receipt == nil {
						if response.ClosedUnreceived == nil || response.Closure == nil || closure == nil {
							return nil, [32]byte{}, protocol.ErrProviderAttemptsUnavailable
						}
						a, _ := json.Marshal(response.Closure)
						b, _ := json.Marshal(closure)
						if !bytes.Equal(a, b) {
							return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
						}
						if err := protocol.VerifyProviderAttemptClosedUnreceived(ctx, *response.ClosedUnreceived, *closure, keys); err != nil {
							return nil, [32]byte{}, err
						}
						_, _ = digest.Write(response.ClosedUnreceived.Body)
						continue
					}
					if response.ClosedUnreceived != nil {
						return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
					}
					decoded, err := protocol.DecodeProviderAttemptReceipt(response.Receipt)
					if err != nil {
						return nil, [32]byte{}, errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
					}
					body, err := protocol.ValidateProviderAttemptReceipt(response.Receipt, keys[decoded.Trail.ServerKeyId])
					if err != nil {
						return nil, [32]byte{}, errors.Join(protocol.ErrProviderAttemptsIntegrity, err)
					}
					if body.Scope == nil || *body.Scope != operator.ReceiptScope || body.Trail.ClientId != record.Identity.ClientId || !bytes.Equal(body.RequestMessage, record.Message) || !bytes.Equal(body.RequestSignature, record.RequestSignature) {
						return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
					}
					_, _ = digest.Write(response.Receipt.Body)
					// Full path is checked on every transition. Previous positions may
					// only advance from the same original pending to confirmed once.
					retain := func(position int, hop *protocol.ProviderAttemptReceiptHop, confirmed bool) error {
						if hop == nil || hop.Seed {
							return nil
						}
						key := eventKey{hotkey: validator.Hotkey, noId: lane.NoId, trail: body.Trail.TrailId, position: position}
						value := event{hop: *hop, confirmed: confirmed}
						if old, exists := events[key]; exists {
							a, b := old.hop, value.hop
							a.ConfirmedMs, b.ConfirmedMs = 0, 0
							a.EgressIpHash, b.EgressIpHash = [32]byte{}, [32]byte{}
							x, _ := json.Marshal(a)
							y, _ := json.Marshal(b)
							if !bytes.Equal(x, y) || old.confirmed && !confirmed || old.confirmed && old.hop.ConfirmedMs != hop.ConfirmedMs {
								return protocol.ErrProviderAttemptsIntegrity
							}
							if old.confirmed && old.hop.EgressIpHash != hop.EgressIpHash {
								return protocol.ErrProviderAttemptsIntegrity
							}
						} else if uint64(len(events)) >= self.authority.MaxRecords*16 {
							return protocol.ErrProviderAttemptsCapacity
						}
						events[key] = value
						return nil
					}
					for position, hop := range body.Trail.Hops {
						if err := retain(position, hop, true); err != nil {
							return nil, [32]byte{}, err
						}
					}
					if err := retain(len(body.Trail.Hops), body.Trail.Pending, false); err != nil {
						return nil, [32]byte{}, err
					}
				}
				head = cut.Header.End
				previousHash = cutHash
				previousEnd = window.EndBlock
			}
		}
	}
	if len(used) != len(responses) {
		return nil, [32]byte{}, protocol.ErrProviderAttemptsIntegrity
	}
	type rowKey struct {
		noId     uint64
		clientId connect.Id
	}
	rows := map[rowKey]*VerifiedProviderAttemptRow{}
	for key, value := range events {
		if err := ctx.Err(); err != nil {
			return nil, [32]byte{}, err
		}
		rowKey := rowKey{noId: key.noId, clientId: value.hop.ClientId}
		row := rows[rowKey]
		if row == nil {
			if uint64(len(rows)) >= self.authority.MaxProviders {
				return nil, [32]byte{}, protocol.ErrProviderAttemptsCapacity
			}
			row = &VerifiedProviderAttemptRow{NoId: key.noId, ClientId: [16]byte(value.hop.ClientId)}
			rows[rowKey] = row
		}
		if value.hop.AssignedMs >= startMs && value.hop.AssignedMs < endMs {
			if row.Assignments == ^uint64(0) {
				return nil, [32]byte{}, protocol.ErrProviderAttemptsCapacity
			}
			row.Assignments++
		}
		if value.confirmed && value.hop.ConfirmedMs >= startMs && value.hop.ConfirmedMs < endMs {
			if row.Confirmations == ^uint64(0) {
				return nil, [32]byte{}, protocol.ErrProviderAttemptsCapacity
			}
			row.Confirmations++
			row.LatencyBuckets[latencyBucket(float64(value.hop.ConfirmedMs-value.hop.AssignedMs))]++
		}
	}
	result := make([]VerifiedProviderAttemptRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].NoId != result[j].NoId {
			return result[i].NoId < result[j].NoId
		}
		return bytes.Compare(result[i].ClientId[:], result[j].ClientId[:]) < 0
	})
	var hash [32]byte
	copy(hash[:], digest.Sum(nil))
	return result, hash, ctx.Err()
}
