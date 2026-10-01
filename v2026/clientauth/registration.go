package clientauth

// Registration is an immutable operation before its first request, then an
// exact server-issued identity before credential installation. Restart never
// guesses a legacy client ID or creates a second operation to clear an error.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

const registrationRecordSchema = "urnetwork-durable-client-registration-v1"

// An operator's deployment or an explicit service role, together with
// client-key ownership, selects the operation. No whole config hash, executable,
// token bytes, token timestamp or mutable policy does. Service fields are
// omitted for historical validator scopes, preserving their canonical bytes.
type RegistrationScope struct {
	Endpoint     string `json:"endpoint"`
	DeploymentId string `json:"deployment_id"`
	ChainId      uint64 `json:"chain_id"`
	GenesisHash  string `json:"genesis_hash"`
	Netuid       uint16 `json:"netuid"`
	ValidatorId  uint64 `json:"validator_id"`
	OperatorNoId uint64 `json:"operator_no_id"`
	ClientKey    string `json:"client_key"`
	ClientRole   string `json:"client_role,omitempty"`
	ClientSlot   string `json:"client_slot,omitempty"`
}

// Application refusals require operator/configuration recovery. Keeping their
// closed code observable never grants replacement registration authority.
type RegistrationRefusedError struct{ Code string }

func (self *RegistrationRefusedError) Error() string {
	return "client registration requires recovery: " + self.Code
}

// A retained token is custody, not evidence that a currently unreachable API
// still authorizes it. Native recovery remains independent of this readiness.
type RegistrationRefreshUnavailableError struct{ cause error }

func (self *RegistrationRefreshUnavailableError) Error() string {
	return "original client refresh remains unavailable"
}
func (self *RegistrationRefreshUnavailableError) Unwrap() error { return self.cause }

// Only a completed remote reply can produce this cause. Local registration or
// credential custody failures retain their original hard errors separately.
type RegistrationResponseIdentityError struct{}

func (self *RegistrationResponseIdentityError) Error() string {
	return "client response changed its original owned identity"
}

type registrationRecord struct {
	Schema    string                        `json:"schema"`
	Scope     RegistrationScope             `json:"scope"`
	Principal registrationPrincipal         `json:"principal"`
	Request   sdk.RegisterNetworkClientArgs `json:"request"`
	ClientId  string                        `json:"client_id,omitempty"`
	DeviceId  string                        `json:"device_id,omitempty"`
}

// Test hooks can fail only after actual durable ownership boundaries. They do
// not replace a read, write, identity validation, server allocation or fsync.
type registrationHooks struct {
	afterRequest func() error
	afterBinding func() error
}

// Callers authorize creation only for independently known new work. Retained
// deployments or provider slots with lost legacy credentials need recovery;
// this flag cannot clear their original operation or identity markers.
func LoadOrRegisterClientJwt(ctx context.Context, api *sdk.Api, networkPath, clientPath, description string, scope RegistrationScope, allowCreate bool) (string, connect.Id, error) {
	return loadOrRegisterClientJwt(ctx, api, networkPath, clientPath, description, scope, allowCreate, registrationHooks{})
}

func loadOrRegisterClientJwt(ctx context.Context, api *sdk.Api, networkPath, clientPath, description string, scope RegistrationScope, allowCreate bool, hooks registrationHooks) (_ string, _ connect.Id, returnErr error) {
	return loadOrRegisterClientJwtInDirectory(ctx, api, networkPath, clientPath, description, scope, allowCreate, hooks, nil)
}

// A provider supplies its key owner's physical directory. Historical callers
// retain their existing independent custody through the nil-directory wrapper.
func loadOrRegisterClientJwtInDirectory(ctx context.Context, api *sdk.Api, networkPath, clientPath, description string, scope RegistrationScope, allowCreate bool, hooks registrationHooks, directory *registrationStore) (_ string, _ connect.Id, returnErr error) {
	return loadOrRegisterClientJwtWithCustody(ctx, api, networkPath, clientPath, description, scope, allowCreate, hooks, directory, nil)
}

// Measurement custody may borrow its separately owned, read-only bootstrap
// directory. The reader is opened only for an already retained operation;
// provider and production-validator wrappers keep their original boundaries.
func loadOrRegisterClientJwtWithCustody(ctx context.Context, api *sdk.Api, networkPath, clientPath, description string, scope RegistrationScope, allowCreate bool, hooks registrationHooks, directory *registrationStore, bootstrapOwner func() (*registrationStore, error)) (_ string, _ connect.Id, returnErr error) {
	if ctx == nil || api == nil {
		return "", connect.Id{}, errors.New("registration has no operation owner")
	}
	if err := ctx.Err(); err != nil {
		return "", connect.Id{}, err
	}
	endpoint, err := api.NetworkClientRegistrationEndpoint()
	if err != nil {
		return "", connect.Id{}, err
	}
	if scope.Endpoint != endpoint {
		return "", connect.Id{}, errors.New("registration scope differs from its configured API or deployment")
	}
	values := []string{scope.ClientKey}
	if scope.ClientRole == "" {
		if scope.ClientSlot != "" || scope.DeploymentId == "" || scope.ChainId == 0 || scope.Netuid == 0 || scope.OperatorNoId == 0 || scope.ValidatorId == 0 {
			return "", connect.Id{}, errors.New("registration scope differs from its configured API or deployment")
		}
		values = append(values, scope.GenesisHash)
	} else {
		if (scope.ClientRole != "provider-v1" && scope.ClientRole != "validator-measurement-v1") ||
			scope.DeploymentId != "" || scope.ChainId != 0 || scope.GenesisHash != "" || scope.Netuid != 0 || scope.ValidatorId != 0 || scope.OperatorNoId != 0 {
			return "", connect.Id{}, errors.New("registration service role is unsupported or mixed with chain authority")
		}
		if scope.ClientRole == "validator-measurement-v1" && (scope.ClientSlot != "direct" || allowCreate) {
			return "", connect.Id{}, errors.New("measurement registration requires its retained direct operation")
		}
		if scope.ClientSlot != "direct" {
			slot, err := hex.DecodeString(strings.TrimPrefix(scope.ClientSlot, "proxy-sha256:"))
			if err != nil || len(slot) != 32 || scope.ClientSlot != "proxy-sha256:"+hex.EncodeToString(slot) {
				return "", connect.Id{}, errors.New("registration service slot is invalid")
			}
		}
	}
	for _, value := range values {
		raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
		if err != nil || len(raw) != 32 || bytes.Equal(raw, make([]byte, 32)) || value != "0x"+hex.EncodeToString(raw) {
			return "", connect.Id{}, errors.New("registration scope has an invalid genesis or client key")
		}
	}
	if bootstrapOwner != nil && (directory == nil || scope.ClientRole != "validator-measurement-v1" || allowCreate) {
		return "", connect.Id{}, errors.New("separate bootstrap custody is restricted to retained measurement work")
	}
	var owner *registrationStore
	if directory == nil {
		owner, err = openRegistrationStore(clientPath)
	} else {
		if (bootstrapOwner == nil && filepath.Dir(networkPath) != filepath.Dir(clientPath)) || filepath.Clean(networkPath) != networkPath || !filepath.IsAbs(networkPath) {
			return "", connect.Id{}, errors.New("service bootstrap differs from its owned custody directory")
		}
		owner, err = openRegistrationStoreForOwner(clientPath, directory)
	}
	if err != nil {
		return "", connect.Id{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, owner.close()) }()
	readToken := ReadToken
	clearRejected := func() error { return clearRejection(clientPath) }
	markRejected := func() error { return MarkRejected(clientPath, networkPath) }
	var bootstrapStore *registrationStore
	if directory != nil {
		readToken = func(path string) (string, error) {
			reader := owner
			if path == networkPath && bootstrapOwner != nil {
				var err error
				bootstrapStore, err = bootstrapOwner()
				if err != nil {
					return "", err
				}
				reader = bootstrapStore
			} else if filepath.Dir(path) != filepath.Dir(clientPath) {
				return "", errors.New("provider credential read escaped its owned directory")
			}
			raw, err := reader.read(filepath.Base(path))
			if err != nil {
				return "", err
			}
			value := strings.TrimSpace(string(raw))
			if value == "" {
				return "", errors.New("provider credential is empty")
			}
			return value, nil
		}
		clearRejected = func() error { return owner.remove(filepath.Base(rejectionPath(clientPath))) }
		markRejected = func() error {
			return errors.Join(owner.remove(filepath.Base(clientPath)), owner.write(filepath.Base(rejectionPath(clientPath)), []byte("blocked")))
		}
	}
	clientName, recordName := filepath.Base(clientPath), filepath.Base(clientPath)+".registration"
	anchorName := recordName + ".started"
	legacyName := recordName + ".existing"
	var record *registrationRecord
	raw, err := owner.read(recordName)
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return "", connect.Id{}, fmt.Errorf("decode original registration: %w", err)
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return "", connect.Id{}, errors.New("original registration has trailing bytes")
		}
		if err := validateRegistrationRecord(record, scope, description); err != nil {
			return "", connect.Id{}, err
		}
		canonical, err := json.Marshal(record)
		if err != nil || !bytes.Equal(canonical, raw) {
			return "", connect.Id{}, errors.New("original registration is not its canonical unambiguous encoding")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", connect.Id{}, err
	}
	anchor, anchorErr := owner.read(anchorName)
	if anchorErr == nil {
		if record == nil {
			return "", connect.Id{}, &RegistrationRefusedError{Code: "registration_custody_missing_after_send"}
		}
		expected, err := registrationOperationAnchor(record)
		if err != nil || !bytes.Equal(anchor, expected) {
			return "", connect.Id{}, errors.New("registration request differs from its durable first-send anchor")
		}
	} else if !errors.Is(anchorErr, os.ErrNotExist) {
		return "", connect.Id{}, anchorErr
	}
	legacy, legacyErr := owner.read(legacyName)
	if legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) {
		return "", connect.Id{}, legacyErr
	}
	if record != nil && legacyErr == nil {
		return "", connect.Id{}, errors.New("registration has both original creation and existing-client custody")
	}
	if raw, err := owner.read(clientName); err == nil {
		token := strings.TrimSpace(string(raw))
		if record != nil {
			if err := validateRegistrationCredential(record, token, record.ClientId, record.DeviceId); err != nil {
				return "", connect.Id{}, err
			}
		} else {
			identity, err := registrationIdentity(token)
			if err != nil || !validRegistrationIdentityId(identity.clientId) || !validRegistrationIdentityId(identity.deviceId) {
				return "", connect.Id{}, errors.New("existing client custody has invalid identity")
			}
			// Observing existing custody consumes no creation authority. Its
			// stable marker survives token loss and excludes bearer timestamps.
			expected, err := json.Marshal(struct {
				Schema    string                `json:"schema"`
				Scope     RegistrationScope     `json:"scope"`
				Principal registrationPrincipal `json:"principal"`
				ClientId  string                `json:"client_id"`
				DeviceId  string                `json:"device_id"`
			}{Schema: "urnetwork-existing-client-v1", Scope: scope, Principal: identity.registrationPrincipal, ClientId: identity.clientId, DeviceId: identity.deviceId})
			if err != nil {
				return "", connect.Id{}, err
			}
			if legacyErr == nil && !bytes.Equal(legacy, expected) {
				return "", connect.Id{}, errors.New("existing client differs from its original identity custody")
			}
			if errors.Is(legacyErr, os.ErrNotExist) {
				if err := owner.write(legacyName, expected); err != nil {
					return "", connect.Id{}, err
				}
			}
		}
		return refreshStoredClientJwtWithCustody(ctx, api, token, func(token string) error { return owner.write(clientName, []byte(token)) }, clearRejected, markRejected, false)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", connect.Id{}, err
	}
	if legacyErr == nil {
		return "", connect.Id{}, &RegistrationRefusedError{Code: "legacy_identity_requires_explicit_recovery"}
	}
	if record == nil && !allowCreate {
		return "", connect.Id{}, &RegistrationRefusedError{Code: "legacy_identity_requires_explicit_recovery"}
	}
	bootstrap, err := readToken(networkPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", connect.Id{}, &RegistrationRefusedError{Code: "network_authentication_missing"}
		}
		return "", connect.Id{}, err
	}
	identity, err := registrationIdentity(bootstrap)
	if err != nil || !validRegistrationIdentityId(identity.NetworkId) || !validRegistrationIdentityId(identity.UserId) || identity.clientId != "" || identity.deviceId != "" {
		return "", connect.Id{}, errors.New("registration bootstrap does not name a network principal")
	}
	if marker, err := readToken(rejectionPath(clientPath)); err == nil {
		if directory != nil || marker == "blocked" || marker == networkCredentialFingerprint(networkPath, bootstrap) {
			return "", connect.Id{}, &RegistrationRefusedError{Code: "client_revoked"}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", connect.Id{}, err
	}
	if record == nil {
		var requestId [32]byte
		if _, err := rand.Read(requestId[:]); err != nil {
			return "", connect.Id{}, err
		}
		scopeRaw, err := json.Marshal(scope)
		if err != nil {
			return "", connect.Id{}, err
		}
		digest := sha256.Sum256(scopeRaw)
		record = &registrationRecord{Schema: registrationRecordSchema, Scope: scope, Principal: identity.registrationPrincipal,
			Request: sdk.RegisterNetworkClientArgs{Schema: sdk.NetworkClientRegistrationSchema, RegistrationId: hex.EncodeToString(requestId[:]), ScopeSha256: hex.EncodeToString(digest[:]), DeviceDescription: description}}
		if err := validateRegistrationRecord(record, scope, description); err != nil {
			return "", connect.Id{}, err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return "", connect.Id{}, err
		}
		if err := owner.write(recordName, raw); err != nil {
			return "", connect.Id{}, err
		}
		if hooks.afterRequest != nil {
			if err := hooks.afterRequest(); err != nil {
				return "", connect.Id{}, err
			}
		}
	}
	if !record.Principal.equal(identity.registrationPrincipal) {
		return "", connect.Id{}, errors.New("registration bootstrap differs from the original principal")
	}
	if errors.Is(anchorErr, os.ErrNotExist) {
		anchor, err := registrationOperationAnchor(record)
		if err != nil {
			return "", connect.Id{}, err
		}
		if err := owner.write(anchorName, anchor); err != nil {
			return "", connect.Id{}, err
		}
	}
	api.SetByJwt(bootstrap)
	if err := owner.check(); err != nil {
		return "", connect.Id{}, err
	}
	if bootstrapStore != nil {
		if err := bootstrapStore.check(); err != nil {
			return "", connect.Id{}, err
		}
	}
	result, err := api.RegisterNetworkClientSyncWithContext(ctx, &record.Request)
	if err != nil {
		return "", connect.Id{}, err
	}
	if result.Error != nil {
		return "", connect.Id{}, &RegistrationRefusedError{Code: result.Error.Code}
	}
	clientId, deviceId := result.ClientId.String(), result.DeviceId.String()
	if err := validateRegistrationCredential(record, result.ByClientJwt, clientId, deviceId); err != nil {
		return "", connect.Id{}, &RegistrationResponseIdentityError{}
	}
	if record.ClientId == "" {
		record.ClientId, record.DeviceId = clientId, deviceId
		raw, err := json.Marshal(record)
		if err != nil {
			return "", connect.Id{}, err
		}
		if err := owner.write(recordName, raw); err != nil {
			return "", connect.Id{}, err
		}
		if hooks.afterBinding != nil {
			if err := hooks.afterBinding(); err != nil {
				return "", connect.Id{}, err
			}
		}
	}
	if err := owner.write(clientName, []byte(result.ByClientJwt)); err != nil {
		return "", connect.Id{}, err
	}
	if err := clearRejected(); err != nil {
		return "", connect.Id{}, err
	}
	api.SetByJwt(result.ByClientJwt)
	id, _ := connect.ParseId(clientId)
	return result.ByClientJwt, id, nil
}

// Independent pre-send custody distinguishes a never-sent initialization from
// a missing operation after possible server mutation. Server-side unique scope
// additionally refuses duplicate allocation if all local custody is lost.
func registrationOperationAnchor(record *registrationRecord) ([]byte, error) {
	original := *record
	original.ClientId, original.DeviceId = "", ""
	raw, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	return []byte(registrationRecordSchema + "\n" + record.Request.RegistrationId + "\n" + hex.EncodeToString(digest[:])), nil
}

// Every restart checks the complete original scope and payload, including the
// old description. Configuration changes cannot silently regenerate either.
func validateRegistrationRecord(record *registrationRecord, scope RegistrationScope, description string) error {
	if record == nil || record.Schema != registrationRecordSchema || !reflect.DeepEqual(record.Scope, scope) || record.Request.DeviceDescription != description || record.Request.DeviceSpec != "" || !validRegistrationIdentityId(record.Principal.NetworkId) || !validRegistrationIdentityId(record.Principal.UserId) {
		return errors.New("original registration scope or principal is invalid or changed")
	}
	raw, err := json.Marshal(scope)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	if record.Request.ScopeSha256 != hex.EncodeToString(digest[:]) {
		return errors.New("original registration scope hash differs")
	}
	if _, err := sdk.EncodeNetworkClientRegistration(&record.Request); err != nil {
		return err
	}
	if (record.ClientId != "" || record.DeviceId != "") && (!validRegistrationIdentityId(record.ClientId) || !validRegistrationIdentityId(record.DeviceId)) {
		return errors.New("original registration has a partial client/device binding")
	}
	return nil
}

// The server-issued client/device claims must agree with both its response
// and any earlier durable binding. There is no response-selected replacement.
func validateRegistrationCredential(record *registrationRecord, token, clientId, deviceId string) error {
	identity, err := registrationIdentity(token)
	if err != nil || !validRegistrationIdentityId(clientId) || !validRegistrationIdentityId(deviceId) || identity.clientId != clientId || identity.deviceId != deviceId || !record.Principal.equal(identity.registrationPrincipal) || record.ClientId != "" && (record.ClientId != clientId || record.DeviceId != deviceId) {
		return errors.New("registered credential differs from its original principal or client/device binding")
	}
	return nil
}
