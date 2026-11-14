package chain

import (
	"strings"
	"testing"
)

func TestRegistrationDecisionTable(t *testing.T) {
	economics := RegistrationEconomics{BurnRao: 1_000_000_000, MinBurnRao: 500_000_000, MaxBurnRao: 100_000_000_000, BurnHalfLifeBlocks: 100, BurnIncreaseMultQ64: "1"}
	coldkey := [32]byte{1}
	other := [32]byte{2}
	for _, testCase := range []struct {
		name       string
		limit      uint64
		registered bool
		owner      [32]byte
		balance    uint64
		wantLimit  uint64
		wantErr    string
	}{
		{name: "default limit is the observed burn", balance: 2_000_000_000, wantLimit: 1_000_000_000},
		{name: "explicit limit above burn", limit: 1_500_000_000, balance: 2_000_000_000, wantLimit: 1_500_000_000},
		{name: "explicit limit below live burn refuses", limit: 900_000_000, balance: 2_000_000_000, wantLimit: 900_000_000, wantErr: "exceeds the burn limit"},
		{name: "insufficient balance refuses", balance: 999_999_999, wantLimit: 1_000_000_000, wantErr: "cannot pay"},
		{name: "already registered under this coldkey is idempotent", registered: true, owner: coldkey, wantLimit: 0},
		{name: "already registered under another coldkey refuses", registered: true, owner: other, wantErr: "already registered under coldkey"},
	} {
		limit, err := RegistrationDecision(economics, testCase.limit, testCase.registered, testCase.owner, coldkey, testCase.balance)
		if testCase.wantErr == "" {
			if err != nil || limit != testCase.wantLimit {
				t.Fatalf("%s: limit=%d err=%v", testCase.name, limit, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
			t.Fatalf("%s: err=%v, want %q", testCase.name, err, testCase.wantErr)
		}
	}
}

func TestAddStakeDecisionTable(t *testing.T) {
	pool := PoolReserves{TaoRao: 2_000_000_000, AlphaInRao: 1_000_000_000} // 2 TAO per alpha
	for _, testCase := range []struct {
		name       string
		registered bool
		balance    uint64
		amount     uint64
		limit      uint64
		wantErr    string
	}{
		{name: "plain add_stake", registered: true, balance: 5_000_000_000, amount: 1_000_000_000},
		{name: "limit at pool price", registered: true, balance: 5_000_000_000, amount: 1_000_000_000, limit: 2_000_000_000},
		{name: "unregistered hotkey", balance: 5_000_000_000, amount: 1, wantErr: "no UID"},
		{name: "zero amount", registered: true, balance: 5_000_000_000, wantErr: "zero"},
		{name: "insufficient balance", registered: true, balance: 1, amount: 2, wantErr: "below"},
		{name: "limit below pool price", registered: true, balance: 5_000_000_000, amount: 1, limit: 1_999_999_999, wantErr: "below the pool price"},
	} {
		err := AddStakeDecision(testCase.registered, testCase.balance, testCase.amount, pool, testCase.limit)
		if testCase.wantErr == "" {
			if err != nil {
				t.Fatalf("%s: %v", testCase.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
			t.Fatalf("%s: err=%v, want %q", testCase.name, err, testCase.wantErr)
		}
	}
}
