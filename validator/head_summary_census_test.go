//go:build linux || darwin

package validator

// Each operator's signed envelope carries its own measurement census and
// bound-client coverage (VERIFY-COHORT-V2.md §2.1, §5.3). The operator bounds
// its verify cohort by that census, so it must be the exact raw census of the
// measurement the cut comes from, never another operator's or an estimate.

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol/headsummary"
	"github.com/urnetwork/connect"
)

func TestHeadSummaryCarriesCensusCount(t *testing.T) {
	keypair := headSummaryTestKeypair(t)
	fixture := newReleaseMeasurementV2TestFixture(t, 2)
	artifact := fixture.artifact
	// operator 10 also carries idle priors, unbound, so the two censuses differ
	providers := &artifact.Inputs[1].Stats.Providers
	zero := releaseHex32([32]byte{})
	for extra := byte(1); len(*providers) == len(artifact.Inputs[0].Stats.Providers) || extra == 1; extra++ {
		id := connect.Id{0xfe, extra}.String()
		*providers = append(*providers, ReleaseProviderMeasurement{ClientID: id, HasPriorQuality: true, PriorQualityPPM: 654_321, LatencyBuckets: make([]uint64, statsLatencyBuckets)})
		artifact.Bindings = append(artifact.Bindings, ReleaseBindingMeasurement{NoID: 10, ClientID: id, FleetID: zero, Hotkey: zero, ClientKey: zero, LocalClientKey: zero, CommitmentHash: zero})
	}
	slices.SortFunc(artifact.Bindings, func(a, b ReleaseBindingMeasurement) int {
		left, right := fmt.Sprintf("%020d:%s", a.NoID, a.ClientID), fmt.Sprintf("%020d:%s", b.NoID, b.ClientID)
		switch {
		case left < right:
			return -1
		case left > right:
			return 1
		}
		return 0
	})
	slices.SortFunc(*providers, func(a, b ReleaseProviderMeasurement) int {
		switch {
		case a.ClientID < b.ClientID:
			return -1
		case a.ClientID > b.ClientID:
			return 1
		}
		return 0
	})
	encoded, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	intent := fixture.intent(t, encoded)
	cuts, err := headSummaryEvidence(artifact)
	if err != nil {
		t.Fatal(err)
	}
	first, second := newHeadSummaryTestOperator(t, nil), newHeadSummaryTestOperator(t, nil)
	finished := make(chan error, 2)
	uploader := startHeadSummaryUploader(t.Context(), keypair.PublicKey(), keypair.Sign, artifact.Policy.PolicyID, artifact.Policy.Steering.MaximumHeadFleets,
		[]*headSummaryTarget{
			{noID: 9, endpoint: first.server.URL + headsummary.Path, credential: headSummaryTestSessionGetter},
			{noID: 10, endpoint: second.server.URL + headsummary.Path, credential: headSummaryTestSessionGetter},
		}, headSummaryHooks{
			now:         func() time.Time { return time.Unix(1_800_000_000, 0) },
			afterUpload: func(_, _ uint64, err error) { finished <- err },
		})
	t.Cleanup(uploader.close)
	(&ReleaseSteerer{headSummary: uploader}).offerHeadSummary(intent, artifact)
	for range 2 {
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(headSummaryTestDeadline):
			t.Fatal("census summary did not upload")
		}
	}
	observedTotal := uint64(0)
	censuses := []uint64{}
	for index, target := range []*headSummaryTestOperator{first, second} {
		summary, err := headsummary.Decode(target.body(0))
		if err != nil {
			t.Fatal(err)
		}
		input := artifact.Inputs[index]
		served := map[string]bool{}
		for _, provider := range input.Stats.Providers {
			if 0 < len(provider.EgressIPHashHexes) {
				served[provider.ClientID] = true
			}
		}
		observed := uint64(0)
		for _, binding := range cuts[index].Bindings {
			if served[binding.ClientId] {
				observed++
			}
		}
		if summary.EvidenceRecipient != input.NoID || summary.CensusProviders == nil || *summary.CensusProviders != uint64(len(input.Stats.Providers)) {
			t.Fatalf("operator %d received a census other than its own measurement's", input.NoID)
		}
		if summary.BoundClientsObserved == nil || *summary.BoundClientsObserved != observed || observed > uint64(len(summary.EvidenceCuts[0].Bindings)) {
			t.Fatalf("operator %d received bound-client coverage other than its cut's", input.NoID)
		}
		observedTotal += observed
		censuses = append(censuses, *summary.CensusProviders)
	}
	if censuses[0] == censuses[1] {
		t.Fatal("the censuses do not differ, so scoping is unproven")
	}
	if observedTotal == 0 {
		t.Fatal("no bound client served a verified hop, so coverage is unproven")
	}
}
