package miner

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/ss58"
)

func validProviderSwarmConfig(t *testing.T) ProviderSwarmConfig {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "member")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jwt", ".provider.jwt", ".provider.key"} {
		value := []byte("token")
		if name == ".provider.key" {
			value = make([]byte, 32)
		}
		if err := os.WriteFile(filepath.Join(stateDir, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wallet, err := ss58.Encode([32]byte{1}, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	return ProviderSwarmConfig{
		Schema: ProviderSwarmSchema, ListenAddress: "127.0.0.1:21081",
		Members: []ProviderSwarmMember{{
			ID: "miner-1", APIURL: "http://127.0.0.1:18081", ConnectURL: "ws://127.0.0.1:19081",
			StateDir: stateDir, Wallet: wallet, SourceIP: "127.64.0.1",
		}},
	}
}

func TestProviderSwarmConfigRejectsAliasedIdentityResources(t *testing.T) {
	config := validProviderSwarmConfig(t)
	duplicate := config.Members[0]
	duplicate.ID = "miner-2"
	config.Members = append(config.Members, duplicate)
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "state_dir") {
		t.Fatalf("aliased state was accepted: %v", err)
	}
	config = validProviderSwarmConfig(t)
	duplicate = config.Members[0]
	duplicate.ID = "miner-2"
	duplicate.StateDir = filepath.Join(t.TempDir(), "missing")
	config.Members = append(config.Members, duplicate)
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "source_ip") {
		t.Fatalf("aliased source was accepted before state validation completed: %v", err)
	}
}

func TestLoadProviderSwarmConfigIsStrict(t *testing.T) {
	config := validProviderSwarmConfig(t)
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "swarm.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProviderSwarmConfig(path); err != nil {
		t.Fatal(err)
	}
	unknown := append(append([]byte(nil), b[:len(b)-1]...), []byte(`,"unknown":true}`)...)
	if err := os.WriteFile(path, unknown, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProviderSwarmConfig(path); err == nil {
		t.Fatal("unknown swarm field was accepted")
	}
}

func TestProviderSwarmStatusFailsClosedOnMissingMember(t *testing.T) {
	config := validProviderSwarmConfig(t)
	swarm, err := NewProviderSwarm(&config)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	response := httptest.NewRecorder()
	swarm.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty swarm status = %d, want 503", response.Code)
	}
	swarm.setRunning("miner-1")
	response = httptest.NewRecorder()
	swarm.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("complete swarm status = %d, want 200", response.Code)
	}
	swarm.setFailure("miner-1", context.Canceled)
	response = httptest.NewRecorder()
	swarm.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed swarm status = %d, want 503", response.Code)
	}
}

func TestProviderSwarmControlDisablesAndRestoresOneRealMemberSlot(t *testing.T) {
	config := validProviderSwarmConfig(t)
	swarm, err := NewProviderSwarm(&config)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	swarm.runCtx = runCtx
	swarm.runCancel = cancel
	swarm.instances["miner-1"] = &providerSwarmInstance{}
	swarm.running["miner-1"] = true
	starts := 0
	swarm.startMember = func(context.Context, ProviderSwarmMember, func(error)) (*providerSwarmInstance, error) {
		starts++
		return &providerSwarmInstance{}, nil
	}
	disable := httptest.NewRequest(http.MethodPost, "/control/miner-1/disable", nil)
	disableResponse := httptest.NewRecorder()
	swarm.ServeHTTP(disableResponse, disable)
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", disableResponse.Code, disableResponse.Body.String())
	}
	status := swarm.status()
	if status.Running != 0 || len(status.Disabled) != 1 || status.Disabled[0] != "miner-1" {
		t.Fatalf("disabled status = %+v", status)
	}
	enable := httptest.NewRequest(http.MethodPost, "/control/miner-1/enable", nil)
	enableResponse := httptest.NewRecorder()
	swarm.ServeHTTP(enableResponse, enable)
	if enableResponse.Code != http.StatusOK || starts != 1 {
		t.Fatalf("enable status = %d starts=%d body=%s", enableResponse.Code, starts, enableResponse.Body.String())
	}
	status = swarm.status()
	if status.Running != 1 || len(status.Disabled) != 0 || len(status.Failures) != 0 {
		t.Fatalf("restored status = %+v", status)
	}
	unknown := httptest.NewRequest(http.MethodPost, "/control/miner-2/disable", nil)
	unknownResponse := httptest.NewRecorder()
	swarm.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusConflict {
		t.Fatalf("unknown member status = %d, want 409", unknownResponse.Code)
	}
}

func TestProviderSwarmDialerUsesExactLoopbackSource(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	settings, err := testEgressDialContextForIP("127.64.8.17")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := settings.DialContext(context.Background(), "tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	serverConnection := <-accepted
	defer serverConnection.Close()
	if host, _, _ := net.SplitHostPort(serverConnection.RemoteAddr().String()); host != "127.64.8.17" {
		t.Fatalf("observed source = %s, want 127.64.8.17", host)
	}
}
