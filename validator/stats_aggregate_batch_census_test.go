//go:build linux || darwin

package validator

// This control forces the independently sampled failed trail to introduce a
// new provider while retaining the full eligible pool and genuine signatures.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect"
)

// One completed M8 trail has seven measured providers, but a separate failed
// assignment may add an eighth. That identity survives a sparse terminal fold.
func TestStatsAggregateTerminalFoldRetainsForcedFailedProvider(t *testing.T) {
	t.Parallel()
	source := newAttemptCutV2SealTestFixture(t, 8, 1, 0)
	completed := source.recordTs[len(source.recordTs)-1]
	completedClientIDs := map[connect.Id]bool{}
	for _, assignment := range completed.Assignments {
		completedClientIDs[assignment.NextHop] = true
	}
	if completed.Disposition != AttemptDispositionComplete || len(completedClientIDs) != 7 || len(source.server.providers) != 16 {
		t.Fatal("original complete M8 path or full provider eligibility pool changed")
	}
	var forcedClientID connect.Id
	for _, provider := range source.server.providers {
		if provider != source.server.providers[0] && !completedClientIDs[provider] {
			forcedClientID = provider
			break
		}
	}
	if forcedClientID == (connect.Id{}) {
		t.Fatal("full pool has no eligible provider outside the completed path")
	}
	source.server.mu.Lock()
	source.server.sampleNextOverride = forcedClientID
	_, ineligibleErr := source.server.sampleNext([]connect.Id{source.server.providers[0], forcedClientID})
	source.server.mu.Unlock()
	if ineligibleErr == nil || !strings.Contains(ineligibleErr.Error(), "not eligible") {
		t.Fatalf("fixture selector bypassed actual eligibility: %v", ineligibleErr)
	}
	source.engine.transport = attemptCutV2SealTestTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
		var envelope struct {
			TrailID *connect.Id `json:"trail_id"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, err
		}
		if envelope.TrailID != nil {
			return nil, errors.New("forced new-provider extension transport failure")
		}
		return source.server.PostVerify(ctx, hop, raw)
	})
	proof, trailErr := source.engine.RunTrail(context.Background())
	source.server.mu.Lock()
	source.server.sampleNextOverride = connect.Id{}
	source.server.mu.Unlock()
	source.engine.transport = source.server
	if trailErr == nil || proof != nil {
		t.Fatalf("real transport failure returned a complete proof: %v", trailErr)
	}
	head, err := source.ledger.Head()
	if err != nil || head.LastSequence != 10 {
		t.Fatalf("complete-plus-failed durable M8 census: %+v error=%v", head, err)
	}
	source.recordTs = nil
	if err := source.ledger.Walk(context.Background(), 1, head.LastSequence, func(record AttemptRecord) error {
		if err := VerifyAttemptRecord(&record, source.ledger.identity, source.key.Public().(ed25519.PublicKey), source.server.serverPublicKeys()); err != nil {
			return err
		}
		source.recordTs = append(source.recordTs, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	failed := source.recordTs[len(source.recordTs)-1]
	if failed.Disposition != AttemptDispositionHopFailure || len(failed.Assignments) != 1 || failed.Assignments[0].NextHop != forcedClientID || completedClientIDs[forcedClientID] || failed.Assignments[0].Confirmed {
		t.Fatalf("forced genuine failed assignment differs: %+v", failed.Assignments)
	}
	seal, objects := newAttemptCutV2SealTestOptions(t, source)
	cut, _, err := SealAttemptCutV2(context.Background(), source.ledger, source.expected, source.policy, source.key, source.bounds, seal)
	if err != nil || cut == nil {
		t.Fatalf("real complete-plus-failed public cut: %v", err)
	}
	config, bounds := source.engine.stats.cfg, statsAggregateTestBounds()
	path := filepath.Join(newAttemptLedgerDiskTestStateDir(t), "aggregate")
	store, err := openStatsAggregateStore(context.Background(), path, source.expected, source.policy, config, bounds, statsAggregateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture := &statsAggregateTestFixture{source: source, cut: *cut, seal: seal, objects: objects, store: store, path: path, config: config, bounds: bounds}
	aggregate, err := store.Replay(context.Background(), *cut, source.expected, source.policy, source.bounds, fixture.replayOptions(t), statsAggregateAdvance{RotateNative: true, ToSettlementEpoch: 43})
	if err != nil || aggregate.ProviderCount != 8 || aggregate.LastAppliedSequence != 10 || aggregate.SettlementEpoch != 43 || aggregate.SettlementFirstSequence != 11 || aggregate.SettlementPriorRoot != cut.Root || aggregate.EgressGeneration != 2 || aggregate.EgressFirstSequence != 11 || aggregate.EgressHashCount != 0 || aggregate.EgressClaimCount != 0 {
		t.Fatalf("forced failed provider was not retained through the exact terminal clocks: %+v error=%v", aggregate, err)
	}
	completedClientIDs[forcedClientID] = true
	rows := statsAggregateTestRows(t, store, aggregate.Generation)
	if len(rows) != 8 {
		t.Fatalf("terminal fold kept %d identities, want all eight actual signed providers", len(rows))
	}
	for _, row := range rows {
		if !completedClientIDs[row.ClientID] || row.WindowPresent || row.Window != (ProviderWindow{}) || row.HasPriorQuality || row.PriorQualityPPM != 0 || row.HasPriorEMA || row.PriorEMA != 0 {
			t.Fatalf("sparse forced provider was replaced or given invented history: %s", row.ClientID)
		}
		delete(completedClientIDs, row.ClientID)
	}
	if len(completedClientIDs) != 0 {
		t.Fatal("terminal fold omitted an actual completed or failed provider")
	}
}
