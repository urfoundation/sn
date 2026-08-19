package crv4

import (
	"crypto/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

// Live conformance tests (the SP-2 gate). Skipped unless SP2_LIVE=1.
//
//	SP2_LIVE=1 [SP2_SUBSTRATE=wss://...] [SP2_NETUID=N] [SP2_BLOCK_TIME=secs] go test ./crv4/ -run TestLive -v
//
// Nothing here submits an extrinsic; the tests read metadata/storage and
// dry-encode a signed commit.
func liveChain(t *testing.T) *Chain {
	t.Helper()
	if os.Getenv("SP2_LIVE") != "1" {
		t.Skip("set SP2_LIVE=1 to run live conformance tests")
	}
	url := os.Getenv("SP2_SUBSTRATE")
	if url == "" {
		url = "wss://test.finney.opentensor.ai:443"
	}
	chain, err := DialChain(url)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	return chain
}

func liveNetuid() uint16 {
	if v := os.Getenv("SP2_NETUID"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil {
			return uint16(n)
		}
	}
	return 1
}

// TestLiveMetadataConformance verifies the live runtime exposes
// commit_timelocked_weights / commit_timelocked_mechanism_weights with the
// expected argument codecs, that every signed extension is either handled or
// zero-size, and that all storage items this package reads exist.
func TestLiveMetadataConformance(t *testing.T) {
	chain := liveChain(t)
	report, err := chain.CheckMetadata()
	if err != nil {
		t.Fatalf("CheckMetadata: %v", err)
	}
	t.Logf("runtime %s spec=%d tx=%d pallet index=%d", report.SpecName, report.SpecVersion, report.TransactionVersion, report.PalletIndex)
	t.Logf("%s: call_index=%d args=%+v", CallCommitTimelocked, report.CommitTimelocked.CallIndex, report.CommitTimelocked.Args)
	t.Logf("%s: call_index=%d args=%+v", CallCommitTimelockedMech, report.CommitMechanism.CallIndex, report.CommitMechanism.Args)
	for _, e := range report.Extensions {
		t.Logf("signed extension %s (%s): handled=%v zeroSize=%v", e.Identifier, e.TypeName, e.Handled, e.ZeroSize)
	}
	for _, p := range report.Problems {
		t.Errorf("metadata problem: %s", p)
	}
	if report.LegacyCrv3Present {
		t.Log("note: legacy commit_crv3_weights still present on this runtime")
	}
	// Pinned expectations from subtensor v3.4.9-424.
	if report.CommitTimelocked.Found && report.CommitTimelocked.CallIndex != 113 {
		t.Logf("note: %s call_index=%d (was 113 at v3.4.9-424)", CallCommitTimelocked, report.CommitTimelocked.CallIndex)
	}
	if report.CommitMechanism.Found && report.CommitMechanism.CallIndex != 118 {
		t.Logf("note: %s call_index=%d (was 118 at v3.4.9-424)", CallCommitTimelockedMech, report.CommitMechanism.CallIndex)
	}
}

// TestLiveCommitRevealVersion asserts the chain's expected
// commit_reveal_version equals the version this package submits by default.
func TestLiveCommitRevealVersion(t *testing.T) {
	chain := liveChain(t)
	v, err := chain.CommitRevealVersion()
	if err != nil {
		t.Fatalf("CommitRevealVersion: %v", err)
	}
	t.Logf("CommitRevealWeightsVersion = %d", v)
	if v != CommitRevealVersion4 {
		t.Errorf("chain expects commit_reveal_version %d, package default is %d", v, CommitRevealVersion4)
	}
}

// TestLiveEpochScheduleAndRound reads the live epoch schedule for the target
// netuid and checks the computed reveal round is in the future relative to
// the drand quicknet clock.
func TestLiveEpochScheduleAndRound(t *testing.T) {
	chain := liveChain(t)
	netuid := liveNetuid()

	state, err := chain.EpochScheduleState(netuid)
	if err != nil {
		t.Fatalf("EpochScheduleState: %v", err)
	}
	t.Logf("netuid %d state: %+v", netuid, *state)
	if state.Tempo == 0 {
		t.Skipf("netuid %d has tempo 0", netuid)
	}
	rpe, err := chain.RevealPeriodEpochs(netuid)
	if err != nil {
		t.Fatalf("RevealPeriodEpochs: %v", err)
	}
	// The public testnet targets standard 12s blocks (measured ~12-15s;
	// SDK default block_time is 12.0). Fast blocks (0.25s) are a localnet
	// docker feature (SP-3).
	blockTime := 12.0
	if v := os.Getenv("SP2_BLOCK_TIME"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			blockTime = f
		}
	}
	now := time.Now()
	round, revealBlock, err := RevealRound(now, state, rpe, blockTime)
	if err != nil {
		t.Fatalf("RevealRound: %v", err)
	}
	current := CurrentDrandRound(now)
	t.Logf("reveal_period_epochs=%d block_time=%v -> reveal_block=%d round=%d (current drand round %d)", rpe, blockTime, revealBlock, round, current)
	if revealBlock <= state.CurrentBlock {
		t.Errorf("reveal block %d not after current block %d", revealBlock, state.CurrentBlock)
	}
	if round <= current {
		t.Errorf("reveal round %d not in the future (current %d)", round, current)
	}
}

// TestLiveDryRunExtrinsicEncoding builds and signs a full CRv4 commit
// extrinsic against the live metadata with a throwaway keypair, without
// submitting. Failure here means the signed-extension set or call encoding
// changed.
func TestLiveDryRunExtrinsicEncoding(t *testing.T) {
	chain := liveChain(t)
	netuid := liveNetuid()

	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		t.Fatal(err)
	}
	kp, err := KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}

	payload := &Payload{Hotkey: kp.PublicKey(), Uids: []uint16{0, 1}, Values: []uint16{100, 65535}, VersionKey: 1}
	enc, err := payload.Encode()
	if err != nil {
		t.Fatal(err)
	}
	round := CurrentDrandRound(time.Now()) + 1200 // ~1h out
	ct, err := Encrypt(enc, round)
	if err != nil {
		t.Fatal(err)
	}

	ext, err := chain.NewCommitExtrinsic(kp, netuid, nil, ct, round, CommitRevealVersion4, 0)
	if err != nil {
		t.Fatalf("NewCommitExtrinsic: %v", err)
	}
	hexEnc, err := EncodeExtrinsic(ext)
	if err != nil {
		t.Fatalf("EncodeExtrinsic: %v", err)
	}
	if len(hexEnc) < 2+2*(len(ct)) {
		t.Errorf("suspiciously short encoded extrinsic (%d hex chars)", len(hexEnc))
	}
	t.Logf("signed commit extrinsic (not submitted): %d bytes", (len(hexEnc)-2)/2)

	mecid := uint8(0)
	if _, err := chain.NewCommitExtrinsic(kp, netuid, &mecid, ct, round, CommitRevealVersion4, 0); err != nil {
		t.Fatalf("NewCommitExtrinsic(mechanism): %v", err)
	}
}
