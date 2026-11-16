// Runtime460 admission preserves the exact replay domains of every companion.
package validator

import (
	"reflect"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Independent protocol artifact literals keep a product pin change observable.
func runtime460ValidatorTestConfig() ReleaseConfig {
	return ReleaseConfig{RuntimeSpec: 460, TransactionVersion: 1, StateVersion: 1,
		RuntimeCodeHash:     "0xa2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d",
		RuntimeMetadataHash: "0x98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c"}
}

// The former-current owner retains its original artifact independently.
func releaseHistorical459TestArtifact() crv4.RuntimeArtifactIdentity {
	cfg := runtime459ValidatorTestConfig()
	return crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 459, TransactionVersion: 1, StateVersion: 1}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
}

// Every owner sees exactly its own artifact and preceding companion artifacts;
// future current pins and pre-companion runtimes cannot change that domain.
func TestReleaseRuntime461PreservesEachOriginalCompanionDomain(t *testing.T) {
	var predecessors []crv4.RuntimeArtifactIdentity
	for _, cfg := range []ReleaseConfig{
		{RuntimeSpec: 455, TransactionVersion: 1, StateVersion: 1, RuntimeCodeHash: "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a", RuntimeMetadataHash: "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"},
		runtime458ValidatorTestConfig(), runtime459ValidatorTestConfig(), runtime460ValidatorTestConfig(), runtime461ValidatorTestConfig(), runtime467ValidatorTestConfig(),
	} {
		owner := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: 1, StateVersion: 1}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
		want := append([]crv4.RuntimeArtifactIdentity{owner}, predecessors...)
		if got := HistoricalReleaseRuntimeArtifacts(owner); !reflect.DeepEqual(got, want) {
			t.Fatalf("runtime%d replay domain=%+v, want %+v", cfg.RuntimeSpec, got, want)
		}
		if err := validateReleaseHistoricalNativeRuntimeConfig(&cfg); err != nil {
			t.Fatal(err)
		}
		if err := validateReleaseNativeRuntimeConfig(&cfg); (err == nil) != (cfg.RuntimeSpec == 467) {
			t.Fatalf("runtime%d gained or lost current authority: %v", cfg.RuntimeSpec, err)
		}
		for _, change := range []func(*crv4.RuntimeArtifactIdentity){
			func(value *crv4.RuntimeArtifactIdentity) { value.Version.SpecVersion = 462 },
			func(value *crv4.RuntimeArtifactIdentity) { value.Version.TransactionVersion++ },
			func(value *crv4.RuntimeArtifactIdentity) { value.Version.StateVersion++ },
			func(value *crv4.RuntimeArtifactIdentity) { value.CodeHash = "0x" + string(make([]byte, 64)) },
			func(value *crv4.RuntimeArtifactIdentity) { value.MetadataHash = "" },
		} {
			foreign := owner
			change(&foreign)
			if got := HistoricalReleaseRuntimeArtifacts(foreign); !reflect.DeepEqual(got, []crv4.RuntimeArtifactIdentity{foreign}) {
				t.Fatal("changed artifact inherited predecessor authority")
			}
		}
		predecessors = append(predecessors, owner)
	}
}
