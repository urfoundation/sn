//go:build dualstacke2e

// The dual-stack hop path end to end (connect IPV6.md C7, VALIDATOR.md §8.2).
// The validator's real TunnelTransport, driven as the trail engine's step
// drives it (the family each hop tries first, and its retries; one tun per
// request; the single-destination client with the fast dial failure), reaches
// in-process connect providers over in-memory routes. Each provider egresses
// from its own loopback addresses to an in-process API whose /verify is the
// server's real controller on the release gate's disposable PostgreSQL and
// Redis. Nothing leaves loopback. Run it the way the release gate runs its
// server database suites, with this tag:
//
//	source server/local/release-gate-services.sh
//	release_gate_services_start "$root" "$workspace" sn/deploy/testnet/release.lock.yml
//	cd server && export WARP_ENV=local WARP_SERVICE=test WARP_DOMAIN=bringyour.com WARP_BLOCK=test WARP_VERSION=0.0.0
//	source "$release_gate_service_root/environment.sh" && source test-env.sh
//	cd ../sn && go test -tags dualstacke2e -run TestDualStackHops ./validator/
//	release_gate_services_cleanup
package validator_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/controller"
	"github.com/urnetwork/server/v2026/model"
	"github.com/urnetwork/server/v2026/router"

	"github.com/urfoundation/sn/v2026/validator"
)

const e2eApiHost = "verify.e2e.test"

// The provider-side API addresses inside each hop's tun: TEST-NET-1 and
// ORCHIDv2, never routed. They pass the client's public-destination policy and
// sit outside the provider's synthetic-speed ranges; a provider maps them to
// the loopback API and never dials them.
var (
	e2eApiV4 = netip.MustParseAddr("192.0.2.80")
	e2eApiV6 = netip.MustParseAddr("2001:20::80")
)

func e2eFamily(addr netip.Addr) int {
	if addr.Unmap().Is4() {
		return 4
	}
	return 6
}

// The loopback API on both families: the generator's client endpoints and the
// server's real /verify, which records each request's source address.
type e2eApi struct {
	v4, v6    netip.AddrPort
	stateLock sync.Mutex
	sources   []netip.Addr
}

func (self *e2eApi) verifySources() []netip.Addr {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]netip.Addr(nil), self.sources...)
}

func newE2EApi(t testing.TB) *e2eApi {
	t.Helper()
	self := &e2eApi{}
	mux := http.NewServeMux()
	mux.HandleFunc("/network/auth-client", func(w http.ResponseWriter, r *http.Request) {
		claims, _ := json.Marshal(map[string]string{"client_id": connect.NewId().String()})
		token := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + base64.RawURLEncoding.EncodeToString(claims) + "."
		_ = json.NewEncoder(w).Encode(map[string]string{"by_client_jwt": token})
	})
	mux.HandleFunc("/connect/control", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"pack":"","error":null}`)
	})
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		if remote, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
			self.stateLock.Lock()
			self.sources = append(self.sources, remote.Addr().Unmap())
			self.stateLock.Unlock()
		}
		router.WrapWithInputNoAuth(controller.Verify, w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	})
	for _, bind := range []string{"127.0.0.1:0", "[::1]:0"} {
		listener, err := net.Listen("tcp", bind)
		if err != nil {
			t.Fatal(err)
		}
		api := &http.Server{Handler: mux}
		go func() { _ = api.Serve(listener) }()
		t.Cleanup(func() { _ = api.Close() })
		addrPort := netip.MustParseAddrPort(listener.Addr().String())
		if e2eFamily(addrPort.Addr()) == 4 {
			self.v4 = addrPort
		} else {
			self.v6 = addrPort
		}
	}
	return self
}

// Answers the API name with both provider-side addresses.
func newE2EDoh(t testing.TB) *httptest.Server {
	t.Helper()
	doh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("dns"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var parser dnsmessage.Parser
		header, err := parser.Start(wire)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		question, err := parser.Question()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true, RecursionAvailable: true})
		_ = builder.StartQuestions()
		_ = builder.Question(question)
		_ = builder.StartAnswers()
		resource := dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 60}
		if question.Name.String() == e2eApiHost+"." {
			switch question.Type {
			case dnsmessage.TypeA:
				_ = builder.AResource(resource, dnsmessage.AResource{A: e2eApiV4.As4()})
			case dnsmessage.TypeAAAA:
				_ = builder.AAAAResource(resource, dnsmessage.AAAAResource{AAAA: e2eApiV6.As16()})
			}
		}
		response, err := builder.Finish()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(response)
	}))
	t.Cleanup(doh.Close)
	return doh
}

// One dial the provider attempted: the destination family and when.
type e2eDial struct {
	family int
	at     time.Time
}

// An in-process provider whose upstream dials leave from its own loopback
// address per family; a family it lacks fails like an IPv4-only host.
type e2eProvider struct {
	id        connect.Id
	egress    map[int]netip.Addr
	client    *connect.Client
	stateLock sync.Mutex
	dials     []e2eDial
	// beforeDial, when set, runs before each upstream dial of a family the
	// provider has; an error fails that dial at once
	beforeDial func(family int) error
}

func (self *e2eProvider) recordedDials() []e2eDial {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]e2eDial(nil), self.dials...)
}

func startE2EProvider(t testing.TB, ctx context.Context, api *e2eApi, egress map[int]netip.Addr) *e2eProvider {
	t.Helper()
	self := &e2eProvider{id: connect.NewId(), egress: egress}
	self.client = connect.NewClient(ctx, self.id, connect.NewNoContractClientOob(), connect.DefaultClientSettings())
	natSettings := connect.DefaultLocalUserNatSettings()
	natSettings.TcpBufferSettings.DialContextSettings = &connect.DialContextSettings{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			destination, err := netip.ParseAddrPort(address)
			if err != nil {
				return nil, err
			}
			family := e2eFamily(destination.Addr())
			self.stateLock.Lock()
			self.dials = append(self.dials, e2eDial{family: family, at: time.Now()})
			self.stateLock.Unlock()
			source, ok := self.egress[family]
			if !ok {
				return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ENETUNREACH}
			}
			if self.beforeDial != nil {
				if err := self.beforeDial(family); err != nil {
					return nil, &net.OpError{Op: "dial", Net: network, Err: err}
				}
			}
			target := api.v4
			if family == 6 {
				target = api.v6
			}
			if destination.Addr() != map[int]netip.Addr{4: e2eApiV4, 6: e2eApiV6}[family] {
				return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
			}
			dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: source.AsSlice()}}
			return dialer.DialContext(ctx, "tcp", target.String())
		},
	}
	nat := connect.NewLocalUserNat(ctx, "e2e provider", natSettings)
	provider := connect.NewRemoteUserNatProvider(self.client, nat, connect.DefaultRemoteUserNatProviderSettings())
	t.Cleanup(func() {
		provider.Close()
		nat.Close()
		self.client.Cancel()
	})
	return self
}

// Routes one registered validator client to the provider in memory, in place
// of the platform relay.
func (self *e2eProvider) wire(client *connect.Client) {
	send, receive := make(chan []byte), make(chan []byte)
	client.RouteManager().UpdateTransport(connect.NewSendGatewayTransport(), []connect.Route{send})
	client.RouteManager().UpdateTransport(connect.NewReceiveGatewayTransport(), []connect.Route{receive})
	client.ContractManager().AddNoContractPeer(self.id)
	self.client.RouteManager().UpdateTransport(connect.NewReceiveGatewayTransport(), []connect.Route{send})
	self.client.RouteManager().UpdateTransport(connect.NewSendClientTransport(connect.DestinationId(client.ClientId())), []connect.Route{receive})
	self.client.ContractManager().AddNoContractPeer(client.ClientId())
}

// Registers the provider at the server: provide modes and one egress feed per
// family it has, as its platform connections would.
func registerE2EProvider(ctx context.Context, id connect.Id, egress []netip.Addr, settings *model.VerifySettings) {
	modes, err := json.Marshal([]model.ProvideMode{model.ProvideModePublic})
	server.Raise(err)
	server.Redis(ctx, func(r server.RedisClient) {
		server.Raise(r.Set(ctx, fmt.Sprintf("{pm_%s}pms", server.Id(id)), modes, 0).Err())
	})
	for _, ip := range egress {
		model.FeedVerifyEgress(ctx, server.Id(id), ip, settings)
	}
}

// A validator transport whose registered clients reach one provider and whose
// hop tuns resolve the API name through the test DoH server, on the host.
//
// One transport is one validator identity, and the tests give each trail its
// own, as a validator's consecutive trails go through providers picked from
// the whole fleet. An identity's next attempt through the provider it has just
// used opens new transfer sequences there once the attempt before has closed
// its contracts (tunnelHopContracts), and on a carrier the provider does not
// know to be reliable, as these in-memory routes, it takes the new sequence
// only after its first packet is resent, a second or more later. That wait is
// no family's: a production hop resolves the API name through its tunnel and
// so waits before its family race starts, while here, resolving on the host,
// it would hold both families' first packets back together.
func newE2ETransport(t testing.TB, ctx context.Context, api *e2eApi, doh *httptest.Server, provider *e2eProvider, validatorId connect.Id) *validator.TunnelTransport {
	t.Helper()
	strategySettings := connect.DefaultClientStrategySettings()
	strategySettings.EnableResilient = false
	// the generator's API calls reach the loopback API by name, with no
	// resolver; every other dial (the unused platform url) is refused
	strategySettings.DialContextSettings = &connect.DialContextSettings{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil || host != e2eApiHost {
				return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp", api.v4.String())
		},
	}
	strategy := connect.NewClientStrategy(ctx, strategySettings)
	t.Cleanup(strategy.Close)
	transport := validator.NewTunnelTransport(ctx, strategy, validator.TunnelTransportConfig{
		ApiUrl:         "http://" + e2eApiHost,
		ConnectUrl:     "http://127.0.0.1:9",
		ByClientJwt:    func() string { return "validator-identity" },
		SourceClientId: validatorId,
	})
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = transport.CloseAndWait(closeCtx)
	})
	validator.SetTunnelTransportE2EHooks(transport,
		func(ctx context.Context, settings *connect.TunSettings) (*connect.Tun, error) {
			return connect.CreateTunWithResolver(ctx, settings, &connect.DnsResolverSettings{
				EnableLocalDoh:   true,
				LocalDohUrlsIpv4: []string{doh.URL},
			})
		},
		provider.wire,
	)
	return transport
}

// A signed SEED body, exactly as the trail engine builds it.
func e2eSeedBody(t testing.TB, validatorId connect.Id, vsk ed25519.PrivateKey) []byte {
	t.Helper()
	vpk := vsk.Public().(ed25519.PublicKey)
	nonce := make([]byte, connect.VerifyNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	message, err := connect.BuildVerifySeedMessage(vpk, nonce, byte(connect.VerifyMMin))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(&connect.VerifySeedArgs{
		ClientId: validatorId, Vpk: vpk, ClientNonce: nonce,
		SeedSig: connect.SignVerifyMessage(vsk, message), M: connect.VerifyMMin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Sends the SEED of the trail-th trail through the provider as the trail
// engine's step does, and requires the server to have resolved the request's
// source to the provider: an unresolved source is a poisoned trail whose
// echoed first hop is synthetic.
func e2eSeed(t testing.TB, ctx context.Context, transport *validator.TunnelTransport, provider *e2eProvider, trail uint64, body []byte) {
	t.Helper()
	response, err := validator.E2EPostHop(ctx, transport, provider.id, body, trail, 0)
	if err != nil {
		t.Fatalf("seed through %s: %v", provider.id, err)
	}
	var assign connect.VerifyAssignResult
	if err := json.Unmarshal(response, &assign); err != nil {
		t.Fatalf("seed response %s: %v", response, err)
	}
	if len(assign.Trail) != 1 || assign.Trail[0] != provider.id {
		t.Fatalf("seed through %s echoed trail %v: the server did not resolve the source to the provider", provider.id, assign.Trail)
	}
}

// e2eWithEachLatencySetting runs body in a fresh server environment twice:
// with the server's hop_latency_from_attributed_attempt off, its default,
// and on (VALIDATOR.md §7.5).
func e2eWithEachLatencySetting(t *testing.T, body func(t testing.TB, ctx context.Context, settings *model.VerifySettings)) {
	for _, fromAttributedAttempt := range []bool{false, true} {
		t.Run(fmt.Sprintf("hop_latency_from_attributed_attempt=%t", fromAttributedAttempt), func(t *testing.T) {
			server.DefaultTestEnv().Run(t, func(t testing.TB) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
				controller.SetVerifyServerKeys([]*controller.VerifyServerKey{{ServerKeyId: 7, PrivateKey: ed25519.NewKeyFromSeed(seed)}})
				settings := model.DefaultVerifySettings()
				settings.HopLatencyFromAttributedAttempt = fromAttributedAttempt
				controller.SetVerifySettings(settings)
				body(t, ctx, settings)
			})
		})
	}
}

// IPV6.md C7, VALIDATOR.md §8.2. The SEEDs of consecutive trails alternate
// their first family: through a dual-stack provider both families resolve at
// the server, and an IPv4-only provider's IPv6-first hops fail over to IPv4 at
// once, on its dial-failure signal, and resolve.
func TestDualStackHopsEndToEnd(t *testing.T) {
	e2eWithEachLatencySetting(t, func(t testing.TB, ctx context.Context, settings *model.VerifySettings) {
		api := newE2EApi(t)
		doh := newE2EDoh(t)
		dualStackV4, dualStackV6 := netip.MustParseAddr("127.0.0.2"), netip.MustParseAddr("::1")
		ipv4OnlyV4 := netip.MustParseAddr("127.0.0.3")
		dualStack := startE2EProvider(t, ctx, api, map[int]netip.Addr{4: dualStackV4, 6: dualStackV6})
		ipv4Only := startE2EProvider(t, ctx, api, map[int]netip.Addr{4: ipv4OnlyV4})
		registerE2EProvider(ctx, dualStack.id, []netip.Addr{dualStackV4, dualStackV6}, settings)
		registerE2EProvider(ctx, ipv4Only.id, []netip.Addr{ipv4OnlyV4}, settings)
		// next hops a SEED may assign; nothing ever dials their egress
		for i := 0; i < 2; i++ {
			registerE2EProvider(ctx, connect.NewId(), []netip.Addr{netip.AddrFrom4([4]byte{203, 0, 113, byte(10 + i)})}, settings)
		}
		_, vsk, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		validatorId := connect.NewId()
		model.SetClientPublicKey(ctx, server.Id(validatorId), vsk.Public().(ed25519.PublicKey))

		const requests = 4
		for _, provider := range []*e2eProvider{dualStack, ipv4Only} {
			before := len(api.verifySources())
			// the SEEDs of consecutive trails, which alternate their first
			// family, each on its own identity (newE2ETransport)
			for trail := range uint64(requests) {
				e2eSeed(t, ctx, newE2ETransport(t, ctx, api, doh, provider, validatorId), provider, trail, e2eSeedBody(t, validatorId, vsk))
			}
			landed := api.verifySources()[before:]
			wantSources := []netip.Addr{dualStackV4, dualStackV6, dualStackV4, dualStackV6}
			if provider == ipv4Only {
				wantSources = []netip.Addr{ipv4OnlyV4, ipv4OnlyV4, ipv4OnlyV4, ipv4OnlyV4}
			}
			if fmt.Sprint(landed) != fmt.Sprint(wantSources) {
				t.Fatalf("provider %s: /verify sources %v, want %v (the first trail IPv4 first, then alternating)", provider.id, landed, wantSources)
			}
			t.Logf("provider %s: four resolved hops arrived from %v", provider.id, landed)
		}

		// the IPv4-only provider was asked for IPv6 on its IPv6-first requests
		// and failed over to IPv4 well inside the fallback delay each time
		dials := ipv4Only.recordedDials()
		fastFallbacks := 0
		for i, dial := range dials {
			if dial.family != 6 {
				continue
			}
			if len(dials) <= i+1 || dials[i+1].family != 4 || connect.DefaultDialFallbackDelay/2 <= dials[i+1].at.Sub(dial.at) {
				t.Fatalf("IPv4-only provider dials %v: IPv6 at %d not followed by IPv4 within %s", dials, i, connect.DefaultDialFallbackDelay/2)
			}
			t.Logf("IPv4-only provider: an IPv6-first hop reached IPv4 %s after its IPv6 dial", dials[i+1].at.Sub(dial.at))
			fastFallbacks++
		}
		if fastFallbacks != requests/2 {
			t.Fatalf("IPv4-only provider saw %d IPv6-first fallbacks, want %d", fastFallbacks, requests/2)
		}
	})
}

// IPV6.md C7, VALIDATOR.md §8.2. Pinned per-family transports (IPV6.md A2 to
// A4) are not enabled yet, so a provider on a dual-stack host runs one
// transport and the server indexes only the family it landed on. A hop the
// trail engine sends first on the other family egresses there, and the server
// answers that it resolved the source to no provider (421); the step resends
// it through the same hop pinned to the indexed family. Every hop resolves:
// on the indexed family at once, on the other after the one resend, for a
// provider with only its IPv4 egress indexed and the mirror case with only
// IPv6.
func TestDualStackHopsResolveThroughOneIndexedFamily(t *testing.T) {
	testIndexedFamilyHops(t, nil, func(trail uint64, indexedFirst bool, indexed, unindexed netip.Addr) []netip.Addr {
		if indexedFirst {
			return []netip.Addr{indexed}
		}
		return []netip.Addr{unindexed, indexed}
	})
}

// VALIDATOR.md §8.2. When the indexed family connects more than the fallback
// delay later than the other, a hop that tries it first still lands on the
// other, and is answered 421; the resend, pinned to the indexed family, waits
// for it instead of falling back again. Every hop resolves after one resend.
func TestDualStackHopsResolveThroughASlowIndexedFamily(t *testing.T) {
	slow := 2 * connect.DefaultDialFallbackDelay
	testIndexedFamilyHops(t, func(indexedFamily int) func(int) error {
		return func(family int) error {
			if family == indexedFamily {
				time.Sleep(slow)
			}
			return nil
		}
	}, func(trail uint64, indexedFirst bool, indexed, unindexed netip.Addr) []netip.Addr {
		return []netip.Addr{unindexed, indexed}
	})
}

// VALIDATOR.md §8.2. When the indexed family fails at once on the resend, the
// step retries it pinned again and never returns to the family the 421 ruled
// out: the hop resolves on its third attempt, and the provider saw the
// unindexed family, the failed indexed dial and the indexed one, in order.
func TestDualStackHopsResolveWhenTheIndexedFamilyFailsOnce(t *testing.T) {
	testIndexedFamilyHops(t, func(indexedFamily int) func(int) error {
		var stateLock sync.Mutex
		armed := false
		return func(family int) error {
			stateLock.Lock()
			defer stateLock.Unlock()
			switch {
			case family != indexedFamily:
				armed = true
			case armed:
				armed = false
				return syscall.ECONNREFUSED
			}
			return nil
		}
	}, func(trail uint64, indexedFirst bool, indexed, unindexed netip.Addr) []netip.Addr {
		if indexedFirst {
			return []netip.Addr{indexed}
		}
		return []netip.Addr{unindexed, indexed}
	})
}

// testIndexedFamilyHops sends the SEEDs of four consecutive trails through a
// provider of each mirror case, whose dials beforeDial (when not nil) shapes
// given its indexed family, and requires each to resolve, having reached the
// server from the sources sources gives.
func testIndexedFamilyHops(
	t *testing.T,
	beforeDial func(indexedFamily int) func(family int) error,
	sources func(trail uint64, indexedFirst bool, indexed, unindexed netip.Addr) []netip.Addr,
) {
	e2eWithEachLatencySetting(t, func(t testing.TB, ctx context.Context, settings *model.VerifySettings) {
		api := newE2EApi(t)
		doh := newE2EDoh(t)
		// next hops a SEED may assign; nothing ever dials their egress
		for i := 0; i < 2; i++ {
			registerE2EProvider(ctx, connect.NewId(), []netip.Addr{netip.AddrFrom4([4]byte{203, 0, 113, byte(20 + i)})}, settings)
		}
		_, vsk, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		validatorId := connect.NewId()
		model.SetClientPublicKey(ctx, server.Id(validatorId), vsk.Public().(ed25519.PublicKey))

		v6 := netip.MustParseAddr("::1")
		for _, legacy := range []struct {
			name    string
			v4      netip.Addr
			indexed netip.Addr
		}{
			{name: "transport on IPv4", v4: netip.MustParseAddr("127.0.0.4"), indexed: netip.MustParseAddr("127.0.0.4")},
			{name: "transport on IPv6", v4: netip.MustParseAddr("127.0.0.5"), indexed: v6},
		} {
			provider := startE2EProvider(t, ctx, api, map[int]netip.Addr{4: legacy.v4, 6: v6})
			if beforeDial != nil {
				provider.beforeDial = beforeDial(e2eFamily(legacy.indexed))
			}
			registerE2EProvider(ctx, provider.id, []netip.Addr{legacy.indexed}, settings)
			unindexed := v6
			if legacy.indexed == v6 {
				unindexed = legacy.v4
			}
			resent := 0
			for trail := range uint64(4) {
				before := len(api.verifySources())
				dialsBefore := len(provider.recordedDials())
				// each trail on its own identity (newE2ETransport); a step's
				// resend rides the identity of its first attempt
				e2eSeed(t, ctx, newE2ETransport(t, ctx, api, doh, provider, validatorId), provider, trail, e2eSeedBody(t, validatorId, vsk))
				landed := api.verifySources()[before:]
				want := sources(trail, validator.E2EHopFirstIpFamily(trail, 0) == e2eFamily(legacy.indexed), legacy.indexed, unindexed)
				if fmt.Sprint(landed) != fmt.Sprint(want) {
					t.Fatalf("%s: trail %d's SEED arrived from %v, want %v (dials %v)", legacy.name, trail, landed, want, provider.recordedDials()[dialsBefore:])
				}
				if 1 < len(want) {
					resent++
				}
				// no attempt after a 421 returns to the family it ruled out
				ruledOut := 0
				for _, dial := range provider.recordedDials()[dialsBefore:] {
					if dial.family == ruledOut {
						t.Fatalf("%s: trail %d dialed family %d again after a 421 ruled it out (dials %v)", legacy.name, trail, ruledOut, provider.recordedDials()[dialsBefore:])
					}
					if dial.family != e2eFamily(legacy.indexed) {
						ruledOut = dial.family
					}
				}
			}
			t.Logf("%s (indexed %s): four SEEDs resolved, %d after a resend on the indexed family", legacy.name, legacy.indexed, resent)
		}
	})
}

// IPV6.md C7, VALIDATOR.md §7.5, §8.2. An EXTEND through a provider with one
// indexed family, the case that failed a whole trail before the 421: the
// trail's second hop tries the provider's other family first, the server
// answers 421 and changes nothing, and the step's resend, pinned to the
// indexed family, confirms the hop. The mirror case, and a co-located pair
// whose other family resolves to the seed provider instead of to no one, do
// the same. With hop_latency_from_attributed_attempt on, the server counts
// the hop's latency from that 421; off, from the ASSIGN a second earlier.
func TestDualStackHopsExtendResolvesThroughOneIndexedFamily(t *testing.T) {
	seedV4, v6 := netip.MustParseAddr("127.0.0.2"), netip.MustParseAddr("::1")
	for _, legacy := range []struct {
		name string
		// the seed provider's indexed egress
		seed map[int]netip.Addr
		// the second hop's egress, and the one address of it the server indexed
		egress  map[int]netip.Addr
		indexed netip.Addr
		// the trail whose second hop tries the unindexed family first
		trail uint64
	}{
		{name: "transport on IPv4", seed: map[int]netip.Addr{4: seedV4}, egress: map[int]netip.Addr{4: netip.MustParseAddr("127.0.0.4"), 6: v6}, indexed: netip.MustParseAddr("127.0.0.4"), trail: 0},
		{name: "transport on IPv6", seed: map[int]netip.Addr{4: seedV4}, egress: map[int]netip.Addr{4: netip.MustParseAddr("127.0.0.5"), 6: v6}, indexed: v6, trail: 1},
		{name: "co-located with the seed provider", seed: map[int]netip.Addr{4: seedV4, 6: v6}, egress: map[int]netip.Addr{4: netip.MustParseAddr("127.0.0.6"), 6: v6}, indexed: netip.MustParseAddr("127.0.0.6"), trail: 0},
	} {
		t.Run(legacy.name, func(t *testing.T) {
			e2eWithEachLatencySetting(t, func(t testing.TB, ctx context.Context, settings *model.VerifySettings) {
				api := newE2EApi(t)
				doh := newE2EDoh(t)
				seedProvider := startE2EProvider(t, ctx, api, legacy.seed)
				hop := startE2EProvider(t, ctx, api, legacy.egress)
				seedAddrs := []netip.Addr{}
				for _, addr := range legacy.seed {
					seedAddrs = append(seedAddrs, addr)
				}
				registerE2EProvider(ctx, seedProvider.id, seedAddrs, settings)
				registerE2EProvider(ctx, hop.id, []netip.Addr{legacy.indexed}, settings)
				_, vsk, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				vpk := vsk.Public().(ed25519.PublicKey)
				validatorId := connect.NewId()
				model.SetClientPublicKey(ctx, server.Id(validatorId), vpk)

				// the SEED's only assignable next hop is the provider under test
				seedResponse, err := validator.E2EPostHop(ctx, newE2ETransport(t, ctx, api, doh, seedProvider, validatorId), seedProvider.id, e2eSeedBody(t, validatorId, vsk), legacy.trail, 0)
				if err != nil {
					t.Fatalf("seed: %v", err)
				}
				var assign connect.VerifyAssignResult
				if err := json.Unmarshal(seedResponse, &assign); err != nil || len(assign.Trail) != 1 || assign.Trail[0] != seedProvider.id || assign.NextHop != hop.id {
					t.Fatalf("seed response %s (%v), want the seed provider confirmed and the provider under test assigned", seedResponse, err)
				}
				// next hops the EXTEND may assign; nothing ever dials their egress
				for i := 0; i < 2; i++ {
					registerE2EProvider(ctx, connect.NewId(), []netip.Addr{netip.AddrFrom4([4]byte{203, 0, 113, byte(30 + i)})}, settings)
				}
				// the hop is slow to be tried: a latency from the ASSIGN holds this
				time.Sleep(time.Second)

				trail := []connect.Id{seedProvider.id, hop.id}
				message, err := connect.BuildVerifyExtendMessage(assign.TrailId, assign.ServerNonce, vpk, byte(assign.M), trail)
				if err != nil {
					t.Fatal(err)
				}
				extendBody, err := json.Marshal(&connect.VerifyExtendArgs{ClientId: validatorId, TrailId: assign.TrailId, Trail: trail, ExtendSig: connect.SignVerifyMessage(vsk, message)})
				if err != nil {
					t.Fatal(err)
				}
				before := len(api.verifySources())
				extendResponse, err := validator.E2EPostHop(ctx, newE2ETransport(t, ctx, api, doh, hop, validatorId), hop.id, extendBody, legacy.trail, 1)
				if err != nil {
					t.Fatalf("extend through the provider: %v", err)
				}
				var next connect.VerifyAssignResult
				if err := json.Unmarshal(extendResponse, &next); err != nil || len(next.Trail) != 2 || next.Trail[1] != hop.id || next.TrailId != assign.TrailId {
					t.Fatalf("extend response %s (%v), want the provider confirmed on the trail", extendResponse, err)
				}
				unindexed := legacy.egress[4]
				if legacy.indexed == unindexed {
					unindexed = legacy.egress[6]
				}
				if landed, want := api.verifySources()[before:], []netip.Addr{unindexed, legacy.indexed}; fmt.Sprint(landed) != fmt.Sprint(want) {
					t.Fatalf("the EXTEND arrived from %v, want %v", landed, want)
				}

				record := model.GetVerifyTrail(ctx, server.Id(assign.TrailId))
				if record == nil || len(record.Hops) != 2 || record.Status != model.VerifyTrailStatusActive {
					t.Fatalf("server trail %+v, want it active with two confirmed hops", record)
				}
				confirmed := record.Hops[1]
				latency := time.Duration(confirmed.ConfirmedMs-confirmed.AssignedMs) * time.Millisecond
				if confirmed.ConfirmedMs < confirmed.AssignedMs || settings.HopLatencyFromAttributedAttempt != (latency < time.Second) {
					t.Fatalf("the server recorded a latency of %s for the hop with hop_latency_from_attributed_attempt=%t", latency, settings.HopLatencyFromAttributedAttempt)
				}
				t.Logf("%s: the EXTEND arrived from %v and resolved; server latency %s", legacy.name, api.verifySources()[before:], latency)
			})
		})
	}
}

// VALIDATOR.md §8.2. A validator step holds the server's unattributed
// attempts plus one attributed attempt, and the validator's unit tests mirror
// the server's limit.
func TestDualStackHopsStepHoldsTheServersRetryableAnswers(t *testing.T) {
	if validator.E2EServerVerifyUnattributedAttemptLimit != model.VerifyUnattributedAttemptLimit {
		t.Fatalf("the validator's tests mirror an unattributed attempt limit of %d, the server's is %d", validator.E2EServerVerifyUnattributedAttemptLimit, model.VerifyUnattributedAttemptLimit)
	}
	if got := validator.E2EDefaultExtendAttempts(); got < model.VerifyUnattributedAttemptLimit+1 {
		t.Fatalf("a step holds %d attempts, want at least the server's %d unattributed ones plus one", got, model.VerifyUnattributedAttemptLimit)
	}
}
