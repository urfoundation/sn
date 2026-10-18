//go:build linux || darwin

// A read that its final custody check refuses leaves no verdict behind.
package validator

import (
	"os"
	"testing"
)

// A fresh store reads the fixture's one retained intent, as at startup. Once
// its entry's own files start to be acquired, the intent file is replaced by
// identical bytes under a new inode. Each entry checks only the custody it
// acquires, so only readV2's final check of the whole custody can see that.
func intentV2RefusedReadTest(t *testing.T) (*IntentStore, *releaseStartupVerified, error) {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	if _, err := fixture.steerer.intents.beginV2(t.Context(), *fixture.intent); err != nil {
		t.Fatalf("actual nonempty V2 begin: %v", err)
	}
	owner, err := newReleaseIntentStoreV2(fixture.runtime)
	if err != nil {
		t.Fatal(err)
	}
	admitted, replaced := 0, false
	owner.v2.referenceReadHooks.step = func(operation string, _ *os.File, _ int) error {
		if operation != "path-admitted" {
			return nil
		}
		// The first acquisition is the intent file; the second, the entry's
		// measurement artifact.
		if admitted++; admitted != 2 {
			return nil
		}
		raw, err := os.ReadFile(owner.path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(owner.path+".synthetic-replacement", raw, 0o600); err != nil {
			return err
		}
		replaced = true
		return os.Rename(owner.path+".synthetic-replacement", owner.path)
	}
	startup := newReleaseStartupVerified()
	current, err := owner.currentV2(withReleaseStartupVerified(t.Context(), startup))
	if !replaced {
		t.Fatal("the intent file was never replaced")
	}
	if err == nil || current != nil {
		t.Fatalf("a read whose intent file was replaced was accepted: %v", err)
	}
	return owner, startup, err
}

// readV2's final check of the whole custody is what keeps a refused read out
// of the verify-once memo and the startup verified prefix.
func TestIntentV2RefusedReadRemembersNoVerifiedHistory(t *testing.T) {
	owner, startup, _ := intentV2RefusedReadTest(t)
	if owner.v2.verified != nil || len(startup.intents) != 0 {
		t.Errorf("a refused read remembered a verified prefix: memo %t, startup prefix %d", owner.v2.verified != nil, len(startup.intents))
	}
}

// The prepared authorization an entry's verification issues is granted only
// by a complete read, so it does not outlive a read that its final check
// refuses.
func TestIntentV2RefusedReadKeepsNoPreparedAuthorization(t *testing.T) {
	owner, _, _ := intentV2RefusedReadTest(t)
	if owner.v2.productionPrepared != nil {
		t.Error("a refused read kept the prepared authorization its entry issued")
	}
}
