// Fleet publication checks roles at each command boundary, including replay
// of pre-tag signed bytes and the pinned-runtime command path.
package miner

import (
	"bytes"
	"errors"
	"testing"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The chain945 command has no mainnet authority document, but still refuses a
// validator before asking for a nonce or signing a tagged fleet commitment.
func TestFleetPinnedPublishCommandRefusesValidatorHotkey(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	expected, _, _, _ := fleetRuntimeCurrentTestArtifactInputs(t)
	fixture.version, fixture.code = expected.Version, expected.CodeHash
	fixture.manifest.ChainID = 945
	fixture.opts["--mainnet-runtime-authority"] = ""
	fixture.opts["--mainnet-runtime-authority-sha256"] = ""
	fixture.registerValidatorRole(t, true)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest)
	if !errors.Is(err, ErrFleetPublishValidatorHotkey) || fixture.count("author_submitAndWatchExtrinsic") != 0 || fixture.count("system_accountNextIndex") != 0 {
		t.Fatalf("pinned command signed or sent from a validator hotkey: %v", err)
	}
}

// The direct authority writer must apply the same check as the durable
// command, rather than relying on the caller to have checked an earlier head.
func TestFleetAuthorityPublishRefusesValidatorHotkey(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.registerValidatorRole(t, true)
	endpoint := fixture.nativeWebsocket(t, false)
	chain, err := crv4.DialChainContext(t.Context(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFleetNative(chain)
	key, err := snchain.LoadKeypairFile(fleetOpt(fixture.opts, "--hotkey_seed_file"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := fixture.manifest.CommitmentHash()
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.authority.publish(t.Context(), chain, key, fixture.manifest.Netuid, hash)
	if !errors.Is(err, ErrFleetPublishValidatorHotkey) || fixture.count("author_submitAndWatchExtrinsic") != 0 || fixture.count("system_accountNextIndex") != 0 {
		t.Fatalf("authority writer signed or sent from a validator hotkey: %v", err)
	}
}

// Pre-tag recovery cannot replay retained bytes after the hotkey acquires a
// validator permit. The immutable pending transaction remains available.
func TestFleetLegacyPublishReplayRefusesValidatorHotkey(t *testing.T) {
	fixture := newFleetMainnetTestFixture(t)
	fixture.opts["--substrate"] = []string{fixture.nativeWebsocket(t, false)}
	legacy, signer := fleetRecoveryTestLegacyPrepared(t, fixture)
	fleetRecoveryTestPut(t, fixture, signer, legacy)
	fixture.registerValidatorRole(t, true)
	err := fleetPublish(fixture.durable.Context, fixture.opts, fixture.manifest)
	if !errors.Is(err, ErrFleetPublishValidatorHotkey) {
		t.Fatalf("legacy validator publication replay was not refused: %v", err)
	}
	retained := fleetRecoveryTestRecord(t, fixture)
	if retained.Stage != "prepared" || retained.Id != legacy.Id || retained.TxHash != legacy.TxHash || !bytes.Equal(retained.Raw, legacy.Raw) || fixture.count("author_submitAndWatchExtrinsic") != 0 {
		t.Fatal("refused legacy replay changed or sent the retained transaction")
	}
}
