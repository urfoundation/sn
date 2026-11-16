//go:build linux || darwin

// Inert multi-entry histories isolate the private memo's predecessor boundary.
// These checks do not supply a native, custody or application proof verdict.
package validator

import (
	"slices"
	"testing"
)

// A two-entry memo stands for the exact prefix a complete read already verified.
func ownedApplicationPrefixTestRead(t *testing.T) (*releaseIntentV2Owner, *releaseIntentV2Read) {
	t.Helper()
	all := intentV2VerifiedHistoryTestEntries(10, 11)
	all[1].Status = "finalized"
	owner := intentV2VerifiedHistoryTestMemo(t, releaseIntentV2VerifiedAuthority{}, all, 0, 1)
	return owner, &releaseIntentV2Read{all: all, file: &steeringIntentFile{Current: all[1]}}
}

// Any changed predecessor, entry count or current-pointer binding invalidates
// the handoff. It cannot use a tail proof to bless unrelated history.
func TestIntentV2OwnedApplicationMemoRequiresExactPrefix(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"prefix changed before capture", "prefix changed after capture", "entry removed", "entry appended", "current alias"} {
		t.Logf("control: %s", name)
		owner, read := ownedApplicationPrefixTestRead(t)
		previous := owner.verified
		original := slices.Clone(previous.digests)
		if name == "prefix changed before capture" {
			read.all[0].SettlementEpoch++
			if owner.captureOwnedApplication(read) != nil {
				t.Fatal("an unverified prefix was captured")
			}
			continue
		}
		candidate := owner.captureOwnedApplication(read)
		if candidate == nil {
			t.Fatal("exact finalized prefix was not captured")
		}
		read.file.Current.Status = "applied"
		switch name {
		case "prefix changed after capture":
			read.all[0].SettlementEpoch++
		case "entry removed":
			read.all = read.all[1:]
		case "entry appended":
			read.all = append(read.all, nativeGapBridgeTestIntent(12, "pending"))
		case "current alias":
			alias := *read.file.Current
			read.file.Current = &alias
		}
		candidate.complete(read, ReleaseMeasurementV2Options{MaxOperators: 17})
		owner.rememberOwnedApplication(candidate, read)
		if len(candidate.digests) != 0 || owner.verified != previous || !slices.Equal(owner.verified.digests, original) {
			t.Fatal("tail verification blessed a changed prefix or current binding")
		}
	}
}

// The changed tail carries the options returned by its own completed proof;
// the earlier tail options stay bound to their original positions.
func TestIntentV2OwnedApplicationMemoRetainsFreshTailOptions(t *testing.T) {
	t.Parallel()
	owner, read := ownedApplicationPrefixTestRead(t)
	candidate := owner.captureOwnedApplication(read)
	if candidate == nil {
		t.Fatal("exact finalized prefix was not captured")
	}
	read.file.Current.Status = "applied"
	candidate.complete(read, ReleaseMeasurementV2Options{MaxOperators: 17})
	owner.rememberOwnedApplication(candidate, read)
	digests := intentV2VerifiedHistoryTestDigests(t, read.all)
	if !slices.Equal(owner.verified.digests, digests) || owner.verified.options[0].MaxOperators != 1 || owner.verified.options[1].MaxOperators != 17 {
		t.Fatal("owned handoff lost its unchanged prefix or fresh tail options")
	}
}
