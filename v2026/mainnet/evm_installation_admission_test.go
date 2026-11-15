// Installation admission preserves the full approved funding reservation and
// the deployment script's separate governance roles before a counted send.
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// The first create cannot consume funds reserved for its already approved
// descendants. Correcting an observation does not change any signed envelope.
func TestEvmReserveAdmissionRetainsSameSenderGraphFunding(t *testing.T) {
	f := newEvmVaultFixture(t)
	f.prepareSigned()
	statePath := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := evmWei(f.config.Plan.MaximumTotalWei)
	if err != nil {
		t.Fatal(err)
	}
	available := new(big.Int).Sub(new(big.Int).Set(reserved), big.NewInt(1))
	f.override = func(method string, params []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", available)
		}
		return result
	}
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code == 0 || !strings.Contains(diagnostic, "maximum liability") || len(f.writes) != 0 {
		t.Fatalf("first create spent descendant funding: code=%d writes=%d %s", code, len(f.writes), diagnostic)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("funding refusal changed original signed custody: %v", err)
	}
	f.stateLock.Lock()
	available.Set(reserved)
	f.stateLock.Unlock()
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Attempts != 1 || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) {
		t.Fatalf("exact graph funding could not submit original bytes: %+v code=%d %s", result, code, diagnostic)
	}
	f.stateLock.Lock()
	available.SetInt64(0)
	f.stateLock.Unlock()
	result, code, diagnostic = f.command("resume", "--online", "--submit")
	if code != 0 || result.Status != "reserve-created" || result.Attempts != 1 || result.Receipt == nil || len(f.writes) != 1 {
		t.Fatalf("later funding loss blocked original receipt recovery: %+v code=%d %s", result, code, diagnostic)
	}
}

// A sealed future relayer reservation stays distinct from the original
// deployer's complete value-and-gas reservation; it does not become executable.
func TestEvmReserveAdmissionKeepsForeignReservationSeparate(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	reserved, err := evmWei(f.config.Plan.MaximumTotalWei)
	if err != nil {
		t.Fatal(err)
	}
	target := common.Address{41}
	f.config.Plan.Actions = append(f.config.Plan.Actions, evmPhaseAction{Id: "evidence-anchor", Sender: common.Address{74}, Nonce: 19,
		To: &target, Data: "0x01020304", ValueWei: "1000", Gas: 21000, FeeCapWei: "10", TipCapWei: "1"})
	f.config.Plan.MaximumTotalWei = new(big.Int).Add(reserved, big.NewInt(211000)).String()
	f.publishConfig()
	f.prepareSigned()
	f.override = func(method string, params []any, result any) any {
		if method == "eth_getBalance" {
			return fmt.Sprintf("0x%x", reserved)
		}
		return result
	}
	result, code, diagnostic := f.command("resume", "--online", "--submit")
	if code != 0 || result.Attempts != 1 || len(f.writes) != 1 || !bytes.Equal(f.writes[0], f.raw) || result.InstallationComplete || result.ActivationReady {
		t.Fatalf("foreign reservation changed first-create funding or authority: %+v code=%d %s", result, code, diagnostic)
	}
}

// Mainnet's four approved roles remain pairwise distinct even when a malformed
// initializer has a valid independent signature. Preview cannot recommend it.
func TestEvmProxyAdmissionRejectsCollapsedDeploymentRoles(t *testing.T) {
	f := newEvmEvidenceFixture(t)
	original := f.config.Plan.Actions[4]
	var artifact contractReleaseArtifact
	for _, value := range evmTestRelease(t).Artifacts {
		if value.Name == "ERC1967Proxy" {
			artifact = value
		}
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		t.Fatal(err)
	}
	creation, err := hex.DecodeString(artifact.Creation)
	if err != nil {
		t.Fatal(err)
	}
	data := common.FromHex(original.Data)
	arguments, err := parsed.Constructor.Inputs.Unpack(data[len(creation):])
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("plan", "--action", "proxy-create"); code != 0 {
		t.Fatalf("distinct deployment roles failed: %d %s", code, diagnostic)
	}
	for _, collision := range []struct {
		name        string
		word        int
		replacement common.Address
	}{
		{name: "owner-deployer", word: 1, replacement: original.Sender},
		{name: "guardian-deployer", word: 2, replacement: original.Sender},
		{name: "oracle-deployer", word: 6, replacement: original.Sender},
		{name: "guardian-owner", word: 2, replacement: common.Address{41}},
		{name: "oracle-owner", word: 6, replacement: common.Address{41}},
		{name: "oracle-guardian", word: 6, replacement: common.Address{42}},
	} {
		initializer := append([]byte(nil), arguments[1].([]byte)...)
		copy(initializer[4+32*collision.word:4+32*(collision.word+1)], common.LeftPadBytes(collision.replacement.Bytes(), 32))
		packed, err := parsed.Pack("", arguments[0], initializer)
		if err != nil {
			t.Fatal(err)
		}
		f.config.Plan.Actions[4] = original
		f.config.Plan.Actions[4].Data = "0x" + artifact.Creation + hex.EncodeToString(packed)
		for _, actionId := range []string{"proxy-create", "reserve-link", "vault-link", "evidence-create"} {
			f.publishConfig()
			if _, code, diagnostic := f.command("plan", "--action", actionId); code != 2 || !strings.Contains(diagnostic, "distinct") {
				t.Errorf("signed %s %s collision accepted: %d %s", actionId, collision.name, code, diagnostic)
			}
			unsigned := f.config
			unsigned.Signature = ""
			raw, err := json.Marshal(unsigned)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.configPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := runMain(f.storageContext(t.Context()), []string{"bootstrap-contracts", "preview", "--action", actionId, "--config", f.configPath}, &stdout, &stderr)
			if code != 2 || !strings.Contains(stderr.String(), "distinct") || stdout.Len() != 0 {
				t.Errorf("unsigned %s %s collision accepted: %d %s", actionId, collision.name, code, stderr.String())
			}
		}
	}
	if len(f.counts) != 0 || len(f.writes) != 0 {
		t.Fatal("role review crossed into network execution")
	}
	for _, name := range []string{evmCreateStateFile, evmProxyCreateStateFile} {
		if _, err := os.Lstat(filepath.Join(f.config.Plan.RunDirectory, name)); !os.IsNotExist(err) {
			t.Fatalf("role review opened %s custody: %v", name, err)
		}
	}
}
