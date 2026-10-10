//go:build linux || darwin

// Actual public startup owns real activation, stores, client files and HTTP
// endpoints. Diagnostic barriers observe decisions; they cannot grant them.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urnetwork/sdk/v2026"

	"gopkg.in/yaml.v3"
)

type productionAuthenticationTestOutput struct {
	wait            chan struct{}
	once            sync.Once
	revoked         chan struct{}
	revokedOnce     sync.Once
	invalid         chan struct{}
	invalidOnce     sync.Once
	unavailable     chan struct{}
	unavailableOnce sync.Once
	signInRejected  chan struct{}
	signInOnce      sync.Once
}

func (self *productionAuthenticationTestOutput) Write([]byte) (int, error) {
	panic("operator authentication used synchronous diagnostics")
}

func (self *productionAuthenticationTestOutput) WriteContext(ctx context.Context, raw []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if bytes.Contains(raw, []byte("code=authentication_pending ")) {
		self.once.Do(func() { close(self.wait) })
	}
	if self.revoked != nil && (bytes.Contains(raw, []byte("code=authentication_revoked ")) || bytes.Contains(raw, []byte("code=jwt_rejection_save_failed "))) {
		self.revokedOnce.Do(func() { close(self.revoked) })
	}
	if self.invalid != nil && bytes.Contains(raw, []byte("code=authentication_recovery_required ")) && bytes.Contains(raw, []byte("cause=hard_error")) {
		self.invalidOnce.Do(func() { close(self.invalid) })
	}
	if self.unavailable != nil && bytes.Contains(raw, []byte("code=authentication_unavailable ")) {
		self.unavailableOnce.Do(func() { close(self.unavailable) })
	}
	if self.signInRejected != nil && bytes.Contains(raw, []byte("code=network_sign_in_rejected ")) && bytes.Contains(raw, []byte("cause=hard_error")) {
		self.signInOnce.Do(func() { close(self.signInRejected) })
	}
	return len(raw), nil
}

// Complete invalid refreshes latch only the API owner, both during startup and
// after genuine successful admission. The real canonical receipt is released
// only after the real API withdrawal; diagnostics are checked separately.
func TestProductionAuthenticationRunReleaseInvalidApiKeepsObservation(t *testing.T) {
	for _, admitted := range []bool{false, true} {
		fixture := newProductionStartupTestFixture(t)
		continuation := fixture.continuation
		pending := continuation.beginAndLoseAcknowledgement(t, fixture.native)
		fixture.native.receiptNumber, fixture.native.applied = 103, true
		continuation.production.head = max(uint64(104), pending.Prepared.RevealBlock)
		continuation.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
		continuation.production.epoch++
		output := &productionAuthenticationTestOutput{wait: make(chan struct{}), invalid: make(chan struct{})}
		withdrawn := make(chan struct{})
		var withdrawnOnce sync.Once
		_, receiptHash := continuation.production.receiptBlock(103)
		fixture.nativeBodyWaitHash, fixture.nativeBodyWait = receiptHash.Hex(), withdrawn
		api := fixture.origins[0]
		api.invalidRefresh = `{"by_jwt":"synthetic invalid token","error":{"message":"synthetic conflicting refusal"}}`
		if admitted {
			api.invalidRefreshAfter = 1
		}
		credentialPath := continuation.production.cfg.Operators[0].ClientJWTFile
		original, err := os.ReadFile(credentialPath)
		if err != nil {
			t.Fatal(err)
		}
		fixture.closePreparation(t)
		observed := context.WithValue(t.Context(), productionAuthenticationReadHooksKey{}, productionAuthenticationReadHooks{observe: func(value productionAuthenticationObservation) {
			if value.noId == 9 && !value.ready && value.recoveryRequired {
				withdrawnOnce.Do(func() { close(withdrawn) })
			}
		}})
		ctx, cancel := context.WithCancel(context.WithValue(observed, releaseDiagnosticHooksKey{}, releaseDiagnosticHooks{writer: output}))
		done := make(chan error, 1)
		go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
		select {
		case err := <-done:
			cancel()
			t.Fatalf("API-local malformed reply stopped retained native observation: admitted=%v error=%v", admitted, err)
		case <-output.wait:
		case <-t.Context().Done():
			cancel()
			err := <-done
			t.Fatalf("invalid API owner never reached original native recovery: %v", err)
		}
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		select {
		case <-withdrawn:
		default:
			t.Fatal("native receipt escaped actual API withdrawal")
		}
		select {
		case <-output.invalid:
		default:
			t.Fatal("API invalid reply was hidden as healthy or transient")
		}
		var stored steeringIntentFile
		if err := json.Unmarshal(fixture.storedIntentBytes(t), &stored); err != nil {
			t.Fatal(err)
		}
		if stored.Current == nil || stored.Current.Status != "applied" || stored.Current.CreatedAt != pending.CreatedAt || stored.Current.Prepared == nil || stored.Current.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || !slices.Equal(fixture.native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
			t.Fatal("API-local failure erased the original signed liability")
		}
		raw, err := os.ReadFile(credentialPath)
		if err != nil || !bytes.Equal(raw, original) {
			t.Fatal("malformed remote reply changed durable credentials")
		}
		api.stateLock.Lock()
		sessions, posts, registrations := api.sessions, api.posts, api.registrationPosts
		api.stateLock.Unlock()
		wantSessions := 1
		if admitted {
			wantSessions = 2
		}
		if sessions != wantSessions || posts != 0 || registrations != 0 {
			t.Fatalf("latched API owner retried or published: sessions=%d posts=%d registrations=%d", sessions, posts, registrations)
		}
	}
}

// A live background401 withdraws only API/trail readiness. Even failure to
// persist its rejection marker is diagnosed before the retained receipt read
// is released; the original native transaction still reaches applied custody.
func TestProductionAuthenticationRunReleaseRevocationKeepsObservation(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	continuation := fixture.continuation
	pending := continuation.beginAndLoseAcknowledgement(t, fixture.native)
	fixture.native.receiptNumber, fixture.native.applied = 103, true
	continuation.production.head = max(uint64(104), pending.Prepared.RevealBlock)
	continuation.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	continuation.production.epoch++
	output := &productionAuthenticationTestOutput{wait: make(chan struct{}), revoked: make(chan struct{})}
	withdrawn := make(chan struct{})
	var withdrawnOnce sync.Once
	_, receiptHash := continuation.production.receiptBlock(103)
	fixture.nativeBodyWaitHash, fixture.nativeBodyWait = receiptHash.Hex(), withdrawn
	api := fixture.origins[0]
	api.rejectRefreshAfter = 1
	marker := continuation.production.cfg.Operators[0].ClientJWTFile + ".rejected"
	api.beforeRefreshReject = func(context.Context) {
		// An actual nonempty directory makes atomic marker replacement fail.
		if err := os.Mkdir(marker, 0700); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(marker, "occupied"), []byte("synthetic retained directory"), 0600); err != nil {
			t.Error(err)
		}
	}
	fixture.closePreparation(t)
	observed := context.WithValue(t.Context(), productionAuthenticationReadHooksKey{}, productionAuthenticationReadHooks{observe: func(value productionAuthenticationObservation) {
		if value.noId == 9 && !value.ready && value.recoveryRequired {
			withdrawnOnce.Do(func() { close(withdrawn) })
		}
	}})
	ctx, cancel := context.WithCancel(context.WithValue(observed, releaseDiagnosticHooksKey{}, releaseDiagnosticHooks{writer: output}))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	select {
	case err := <-done:
		t.Fatalf("live revocation stopped original native observation: %v", err)
	case <-output.wait:
	case <-t.Context().Done():
		cancel()
		err := <-done
		t.Fatalf("revoked operator did not retain observed native work: %v", err)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-withdrawn:
	default:
		t.Fatal("native receipt escaped actual revocation and persistence attempt")
	}
	select {
	case <-output.revoked:
	default:
		t.Fatal("native receipt escaped the actual live revocation boundary")
	}
	var stored steeringIntentFile
	if err := json.Unmarshal(fixture.storedIntentBytes(t), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Current == nil || stored.Current.Status != "applied" || stored.Current.CreatedAt != pending.CreatedAt || stored.Current.Prepared == nil || stored.Current.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || !slices.Equal(fixture.native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("live revocation erased original pending work")
	}
	if info, err := os.Stat(marker); err != nil || !info.IsDir() {
		t.Fatal("rejection persistence fault did not occur at the real filesystem")
	}
	for _, api := range fixture.origins {
		api.stateLock.Lock()
		posts := api.posts
		api.stateLock.Unlock()
		if posts != 0 {
			t.Fatal("revocation permitted fresh publication")
		}
	}
}

// A fresh service actually starts a trail before the immediate background
// refresh rejects its client. The independent child context joins that trail;
// its cancellation does not terminate the enclosing native service.
func TestProductionAuthenticationRunReleaseRevocationJoinsActiveTrail(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	fixture.selectEmptyDeployment(t)
	api := fixture.origins[0]
	api.rejectRefreshAfter = 1
	api.beforeRefreshReject = func(ctx context.Context) {
		select {
		case <-api.seedRead:
		case <-ctx.Done():
		case <-api.release:
		}
	}
	output := &productionAuthenticationTestOutput{wait: make(chan struct{}), revoked: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), releaseDiagnosticHooksKey{}, releaseDiagnosticHooks{writer: output}))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	select {
	case err := <-done:
		t.Fatalf("fresh lifecycle ended before actual trail revocation: %v", err)
	case <-output.revoked:
	case <-t.Context().Done():
		cancel()
		err := <-done
		t.Fatalf("actual running trail did not reach revocation: %v", err)
	}
	select {
	case <-api.seedRead:
	default:
		t.Fatal("revocation fixture did not reach a real active trail")
	}
	// The preparation worker must now expose its persistent readiness wait,
	// rather than a transport closure terminating the whole service.
	select {
	case err := <-done:
		t.Fatalf("operator child cancellation stopped independent work: %v", err)
	case <-output.wait:
	case <-t.Context().Done():
		cancel()
		err := <-done
		t.Fatalf("revoked live operator did not expose readiness wait: %v", err)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(fixture.native.broadcasts) != 0 {
		t.Fatal("revoked operator authorized a fresh native send")
	}
}

// Canceling an operator-owned trail does not erase a concurrent real evidence
// failure when the engine joins. The parent must receive its original cause.
func TestProductionAuthenticationChildCancelPreservesTrailFailure(t *testing.T) {
	child, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	canary := errors.New("synthetic durable proof failure")
	owner := &productionOperatorAuthentication{admitted: make(chan struct{}), trailContext: child, cancelTrails: cancel,
		engine: releaseShutdownTrailFunc(func(context.Context, int) error {
			close(entered)
			<-release
			return errors.Join(context.Canceled, fatalTrailState("synthetic proof", canary))
		}),
	}
	owner.ready.Store(true)
	close(owner.admitted)
	done := make(chan error, 1)
	go func() { done <- owner.Run(t.Context(), 1) }()
	<-entered
	owner.ready.Store(false)
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, canary) {
		t.Fatal("operator cancellation masked the engine's durable failure")
	}
}

// A missing legacy credential cannot justify another allocation. It also
// cannot prevent observing and applying an already signed native liability.
func TestProductionAuthenticationRunReleaseObservesRetainedWithoutClient(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	continuation := fixture.continuation
	pending := continuation.beginAndLoseAcknowledgement(t, fixture.native)
	fixture.native.receiptNumber, fixture.native.applied = 103, true
	continuation.production.head = max(uint64(104), pending.Prepared.RevealBlock)
	continuation.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	continuation.production.epoch++
	for _, op := range continuation.production.cfg.Operators {
		if err := os.Remove(op.ClientJWTFile); err != nil {
			t.Fatal(err)
		}
	}
	fixture.closePreparation(t)
	output := &productionAuthenticationTestOutput{wait: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), releaseDiagnosticHooksKey{}, releaseDiagnosticHooks{writer: output}))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	select {
	case err := <-done:
		t.Fatalf("missing operator credential stopped retained native observation: %v", err)
	case <-output.wait:
	case <-t.Context().Done():
		cancel()
		err := <-done
		t.Fatalf("retained observation did not reach its actual authentication gate: %v", err)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var stored steeringIntentFile
	if err := json.Unmarshal(fixture.storedIntentBytes(t), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Current == nil || stored.Current.Status != "applied" || stored.Current.CreatedAt != pending.CreatedAt || stored.Current.SubnetEpoch != pending.SubnetEpoch || stored.Current.Prepared == nil || stored.Current.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || stored.Current.FinalizedBlock != 103 || stored.Current.ApplicationBlock == 0 || !slices.Equal(fixture.native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("authentication outage lost original exact receipt/application custody or manufactured another send")
	}
	for _, api := range fixture.origins {
		api.stateLock.Lock()
		posts, registrations, sessions := api.posts, api.registrationPosts, api.sessions
		api.stateLock.Unlock()
		if posts != 0 || registrations != 0 || sessions != 0 {
			t.Fatal("unavailable legacy identity authorized new API work")
		}
		select {
		case <-api.seedRead:
			t.Fatal("missing authentication enabled trails")
		default:
		}
	}
	fixture.stateLock.Lock()
	latest, writes := fixture.latestReads, fixture.writeRequests
	fixture.stateLock.Unlock()
	if latest != 0 || writes != 0 {
		t.Fatal("authentication wait initiated fresh native activity")
	}
}

// The actual root waits behind a lost versioned reply, then reuses its durable
// request and reaches genuine initial publication/trail readiness on recovery.
func TestProductionAuthenticationRunReleaseRegistrationRecovers(t *testing.T) {
	fixture := newProductionStartupTestFixtureWithRegistration(t, true)
	fixture.selectEmptyDeployment(t)
	for index, op := range fixture.continuation.production.cfg.Operators {
		if err := os.Remove(op.ClientJWTFile); err != nil {
			t.Fatal(err)
		}
		fixture.origins[index].registrationUnavailable = true
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	for index, api := range fixture.origins {
		select {
		case err := <-done:
			t.Fatalf("first registration stopped the public lifecycle: %v", err)
		case <-api.registrationRead:
		case <-t.Context().Done():
			cancel()
			err := <-done
			t.Fatalf("registration request did not reach its real endpoint: %v", err)
		}
		op := fixture.continuation.production.cfg.Operators[index]
		if raw, err := os.ReadFile(op.ClientJWTFile + ".registration"); err != nil || len(raw) == 0 {
			t.Fatal("first send preceded durable request custody")
		}
		if raw, err := os.ReadFile(op.ClientJWTFile + ".registration.started"); err != nil || len(raw) == 0 {
			t.Fatal("first send preceded durable operation anchor")
		}
		api.stateLock.Lock()
		posts := api.posts
		api.stateLock.Unlock()
		if posts != 0 {
			t.Fatal("missing registration reply enabled publication")
		}
		select {
		case <-api.seedRead:
			t.Fatal("missing registration reply enabled trails")
		default:
		}
	}
	for _, api := range fixture.origins {
		api.stateLock.Lock()
		api.registrationUnavailable = false
		api.stateLock.Unlock()
	}
	select {
	case err := <-done:
		t.Fatalf("replayed registration did not reach readiness: %v", err)
	case <-fixture.origins[0].seedRead:
	case <-fixture.origins[1].seedRead:
	case <-t.Context().Done():
		cancel()
		err := <-done
		t.Fatalf("registration recovery did not reach genuine trail readiness: %v", err)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for index, api := range fixture.origins {
		api.stateLock.Lock()
		requests := api.registrationPosts
		api.stateLock.Unlock()
		if requests < 2 {
			t.Fatal("registration recovery did not replay its original operation")
		}
		if raw, err := os.ReadFile(fixture.continuation.production.cfg.Operators[index].ClientJWTFile); err != nil || string(raw) != api.credential {
			t.Fatal("ready operator lacks the durable server-issued credential")
		}
	}
	fixture.stateLock.Lock()
	writes := fixture.writeRequests
	fixture.stateLock.Unlock()
	if writes != 0 || len(fixture.native.broadcasts) != 0 {
		t.Fatal("registration recovery invented a native send")
	}
}

// Omitted/false preserves the exact pre-change JSON grammar that existing
// signed complete configurations hash; only explicit opt-in adds authority.
func TestProductionAuthenticationCreationOptInPreservesHistoricalEncoding(t *testing.T) {
	const original = `{"no_id":9,"api_url":"https://synthetic.invalid","connect_url":"wss://synthetic.invalid","artifact_signer":"a","state_dir":"s","network_jwt_file":"n","client_jwt_file":"c","client_key_seed_file":"k","concurrency":1}`
	var op OperatorConfig
	if err := json.Unmarshal([]byte(original), &op); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(op)
	if err != nil || string(raw) != original {
		t.Fatal("default registration field changed historical signed configuration bytes")
	}
	yamlRaw, err := yaml.Marshal(op)
	if err != nil || bytes.Contains(yamlRaw, []byte("allow_client_registration")) {
		t.Fatal("omitted creation authority did not survive YAML encoding")
	}
	op.AllowClientRegistration = true
	raw, err = json.Marshal(op)
	if err != nil || !bytes.Contains(raw, []byte(`"allow_client_registration":true`)) {
		t.Fatal("fresh creation authority escaped the signed configuration")
	}
	fixture := newOwnerRecycleProductionTestFixture(t)
	fixture.cfg.Operators[0].AllowClientRegistration = true
	if err := validateOwnerRecycleProductionConfig(fixture.cfg); err == nil {
		t.Fatal("unsigned creation opt-in reused old production approval")
	}
}

// The actual loop owns its classification: a durable authentication wait
// remains observable beyond the hard-failure budget, but never masks a defect.
func TestProductionAuthenticationWaitPreservesHardFailures(t *testing.T) {
	for _, hard := range []bool{false, true} {
		canary := errors.New("synthetic independent custody failure")
		calls := 0
		err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error {
			calls++
			wait := &productionOperatorAuthenticationPending{noId: 9, nativeEpoch: 7, epochKnown: true}
			if hard {
				return errors.Join(wait, canary)
			}
			return wait
		}, func() bool { return calls < releaseSteeringFailureLimit+2 }, nil)
		if hard {
			if !errors.Is(err, canary) || calls != releaseSteeringFailureLimit {
				t.Fatal("authentication wait hid a real joined failure")
			}
		} else if err != nil || calls != releaseSteeringFailureLimit+2 {
			t.Fatal("authentication wait consumed the terminal failure budget")
		}
	}
}

// A known invalid API reply can disable its own owner, but a joined filesystem
// or durable-proof failure must never disappear into that localized state.
func TestProductionAuthenticationLocalFailureKeepsSharedIntegrityHard(t *testing.T) {
	invalid := &sdk.ClientControlResponseError{}
	shared := errors.New("synthetic shared custody close failure")
	for _, sample := range []struct {
		err   error
		local bool
	}{
		{err: invalid, local: true},
		{err: &clientauth.RegistrationResponseIdentityError{}, local: true},
		{err: errors.Join(invalid, context.DeadlineExceeded), local: true},
		{err: errors.Join(invalid, shared)},
		{err: errors.Join(invalid, context.Canceled)},
		{err: shared},
	} {
		if productionRegistrationLocalFailure(sample.err) != sample.local {
			t.Fatal("API-local refusal hid an unrelated shared integrity cause")
		}
		if wait, _ := productionRegistrationWait(sample.err); wait {
			t.Fatal("complete invalid API response became automatic retry")
		}
	}
	owner := &productionOperatorAuthentication{}
	owner.recoveryRequired.Store(true)
	runtime := &releaseRuntimeV2{runtimes: []*releaseOperatorRuntime{{authentication: owner, measurement: &ReleaseMeasurementContext{NoID: 9}}}}
	var pending *productionOperatorAuthenticationPending
	if !errors.As(runtime.authenticationPending(nil), &pending) || !pending.hard {
		t.Fatal("latched invalid API lost its hard readiness diagnosis")
	}
}

// Real public startup opens the original nonempty owner, then both configured
// refresh HTTP reads exhaust their actual operation context. The native receipt
// is withheld until the unavailable diagnostic, so applied proves observation
// remains live after exhaustion rather than finishing before the API fails.
func TestProductionAuthenticationRunReleaseRefreshTimeoutKeepsObservation(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	continuation := fixture.continuation
	pending := continuation.beginAndLoseAcknowledgement(t, fixture.native)
	fixture.native.receiptNumber, fixture.native.applied = 103, true
	continuation.production.head = max(uint64(104), pending.Prepared.RevealBlock)
	continuation.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	continuation.production.epoch++
	output := &productionAuthenticationTestOutput{wait: make(chan struct{}), unavailable: make(chan struct{})}
	_, receiptHash := continuation.production.receiptBlock(103)
	fixture.nativeBodyWaitHash, fixture.nativeBodyWait = receiptHash.Hex(), output.unavailable
	entered := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	for index, api := range fixture.origins {
		var once sync.Once
		api.beforeRefresh = func(ctx context.Context) {
			once.Do(func() { close(entered[index]) })
			select {
			case <-ctx.Done():
			case <-api.release:
			}
		}
	}
	fixture.closePreparation(t)
	attempts := make(chan context.CancelFunc, 16)
	hooks := productionAuthenticationReadHooks{withTimeout: func(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		if duration != 5*time.Minute || duration < time.Minute {
			t.Error("production authentication changed its approved read budget")
		}
		base, cancel := context.WithCancel(parent)
		select {
		case attempts <- cancel:
		default:
			t.Error("unexpected repeated authentication operation")
		}
		return productionAuthenticationDeadlineTestContext{Context: base}, cancel
	}}
	ctx, cancel := context.WithCancel(context.WithValue(context.WithValue(t.Context(), releaseDiagnosticHooksKey{}, releaseDiagnosticHooks{writer: output}), productionAuthenticationReadHooksKey{}, hooks))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	for _, read := range entered {
		select {
		case <-read:
		case err := <-done:
			t.Fatalf("API timeout prevented actual retained observation: %v", err)
		case <-t.Context().Done():
			cancel()
			<-done
			t.Fatal(t.Context().Err())
		}
	}
	for range entered {
		(<-attempts)()
	}
	select {
	case <-output.wait:
	case err := <-done:
		t.Fatalf("API timeout stopped native receipt observation: %v", err)
	case <-t.Context().Done():
		cancel()
		<-done
		t.Fatal(t.Context().Err())
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-output.unavailable:
	default:
		t.Fatal("actual API exhaustion lost its unavailable cause")
	}
	var stored steeringIntentFile
	if err := json.Unmarshal(fixture.storedIntentBytes(t), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Current == nil || stored.Current.Status != "applied" || stored.Current.CreatedAt != pending.CreatedAt || stored.Current.Prepared == nil || stored.Current.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || stored.Current.FinalizedBlock != 103 || !slices.Equal(fixture.native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatal("exhausted API read erased or replaced original native custody")
	}
	for index, api := range fixture.origins {
		api.stateLock.Lock()
		posts, registrations := api.posts, api.registrationPosts
		api.stateLock.Unlock()
		raw, err := os.ReadFile(continuation.production.cfg.Operators[index].ClientJWTFile)
		if err != nil || string(raw) != api.credential || posts != 0 || registrations != 0 {
			t.Fatal("API timeout changed credential or allowed publication/allocation")
		}
		select {
		case <-api.seedRead:
			t.Fatal("API timeout enabled trails")
		default:
		}
	}
}

type productionAuthenticationDeadlineTestContext struct{ context.Context }

func (self productionAuthenticationDeadlineTestContext) Err() error {
	if self.Context.Err() != nil {
		return context.DeadlineExceeded
	}
	return nil
}
func (self productionAuthenticationDeadlineTestContext) Value(key any) any {
	return context.WithoutCancel(self.Context).Value(key)
}

// An operator that rejects the network JWT at registration latches only that
// operator, with the code that names a new sign-in rather than custody
// recovery; native observation continues.
func TestProductionAuthenticationRunReleaseNamesARejectedSignIn(t *testing.T) {
	fixture := newProductionStartupTestFixtureWithRegistration(t, true)
	fixture.selectEmptyDeployment(t)
	for index, op := range fixture.continuation.production.cfg.Operators {
		if err := os.Remove(op.ClientJWTFile); err != nil {
			t.Fatal(err)
		}
		fixture.origins[index].registrationRejected = true
	}
	output := &productionAuthenticationTestOutput{wait: make(chan struct{}), signInRejected: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), releaseDiagnosticHooksKey{}, releaseDiagnosticHooks{writer: output}))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	select {
	case <-output.signInRejected:
	case err := <-done:
		t.Fatalf("a rejected sign-in stopped the public lifecycle: %v", err)
	case <-t.Context().Done():
		cancel()
		err := <-done
		t.Fatalf("a rejected sign-in was not named: %v", err)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestProductionRegistrationRecoveryCodeNamesARejectedSignIn(t *testing.T) {
	rejected := fmt.Errorf("registration: %w", &clientauth.NetworkCredentialRejectedError{})
	if productionRegistrationRecoveryCode(rejected) != "network_sign_in_rejected" {
		t.Fatal("a rejected sign-in lost its code")
	}
	if productionRegistrationRecoveryCode(&sdk.ClientControlResponseError{}) != "authentication_recovery_required" {
		t.Fatal("custody recovery lost its code")
	}
	if !productionRegistrationLocalFailure(rejected) {
		t.Fatal("a rejected sign-in did not latch its operator")
	}
	if wait, _ := productionRegistrationWait(rejected); wait {
		t.Fatal("a rejected sign-in became an automatic retry")
	}
	if productionRegistrationLocalFailure(errors.Join(rejected, errors.New("synthetic shared custody failure"))) {
		t.Fatal("a rejected sign-in hid a shared integrity cause")
	}
}
