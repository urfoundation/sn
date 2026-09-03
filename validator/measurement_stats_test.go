package validator

import (
	"testing"

	"github.com/urnetwork/connect"
)

func TestExactPoolQualityRejectsCounterOverflow(t *testing.T) {
	clientID := connect.NewId()
	stats := VerifiedReleaseStats{Providers: map[connect.Id]VerifiedReleaseProvider{
		clientID: {ClientID: clientID, QualityPPM: 1_000_000, HasQuality: true, Exposure: ^uint64(0)},
	}}
	if _, err := ExactPoolQualityFromReleaseStats(stats, nil); err == nil {
		t.Fatal("overflowing exposure-weighted quality unexpectedly succeeded")
	}
}

func TestExactPoolQualityAcceptsMaximumRepresentableCounters(t *testing.T) {
	clientID := connect.NewId()
	stats := VerifiedReleaseStats{Providers: map[connect.Id]VerifiedReleaseProvider{
		clientID: {ClientID: clientID, QualityPPM: 1, HasQuality: true, Exposure: ^uint64(0)},
	}}
	quality, err := ExactPoolQualityFromReleaseStats(stats, nil)
	if err != nil {
		t.Fatal(err)
	}
	if quality != 1 {
		t.Fatalf("maximum representable pool quality = %d, want 1", quality)
	}
}
