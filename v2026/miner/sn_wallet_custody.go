// Wallet consent custody retains exact signed handoffs before their first send.
// The local inventory may omit rotations performed elsewhere; it is not an
// independently approved complete wallet history.
package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/urfoundation/sn/v2026/protocol"
)

const snWalletConsentSchema = "urnetwork-provider-wallet-consent-custody-v1"
const snWalletConsentMaxApiUrlBytes = 4 * 1024
const snWalletConsentMaxBytes = protocol.MaxWalletMappingHistory*(protocol.MaxWalletMappingConsentBytes+64) + 6*snWalletConsentMaxApiUrlBytes + 1024

// Acknowledgement changes only local delivery state, never the original bytes.
type snWalletConsentRecord struct {
	Original     protocol.WalletMappingConsent `json:"original"`
	Acknowledged bool                          `json:"acknowledged"`
}

// One exact selected endpoint owns every original in this bounded inventory.
type snWalletConsentJournal struct {
	Schema  string                  `json:"schema"`
	ApiUrl  string                  `json:"api_url"`
	Records []snWalletConsentRecord `json:"records"`
}

// The caller keeps this owner open through response acknowledgement and joins
// all uses before close. Methods are not safe for concurrent use. A failed
// publication poisons this owner, so uncertainty can never license a send.
type snWalletConsentOwner struct {
	store     *snWalletConsentStore
	apiUrl    string
	digest    [32]byte
	hasDigest bool
	failed    error
	closeErr  error
}

// Only explicit wallet invocation creates the private sibling directory. The
// selected credential must already exist; neither credentials nor preparation
// declarations are created here. Existing incomplete custody is never reborn.
func openSnWalletConsent(ctx context.Context, tokenPath, apiUrl string) (_ *snWalletConsentOwner, returnErr error) {
	if ctx == nil {
		return nil, protocol.ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if apiUrl == "" || len(apiUrl) > snWalletConsentMaxApiUrlBytes {
		return nil, errors.New("wallet consent requires one bounded explicit API URL")
	}
	store, created, err := openSnWalletConsentStore(tokenPath)
	if err != nil {
		return nil, err
	}
	self := &snWalletConsentOwner{store: store, apiUrl: apiUrl}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, self.close())
		}
	}()
	if created {
		journal := &snWalletConsentJournal{Schema: snWalletConsentSchema, ApiUrl: apiUrl, Records: []snWalletConsentRecord{}}
		if err := self.publish(ctx, journal); err != nil {
			return nil, err
		}
	}
	if _, _, _, err := self.read(ctx); err != nil {
		return nil, err
	}
	return self, nil
}

// Return a value copy so callers cannot mutate retained delivery state. Both
// pending and acknowledged originals are replayed exactly by the HTTP owner.
func (self *snWalletConsentOwner) load(ctx context.Context) (*protocol.WalletMappingConsent, bool, error) {
	journal, _, _, err := self.read(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(journal.Records) == 0 {
		return nil, false, nil
	}
	record := journal.Records[len(journal.Records)-1]
	return &record.Original, record.Acknowledged, nil
}

// Retrying the current original is idempotent. A different pending original
// cannot be replaced, even by a newly valid challenge for the same wallet.
func (self *snWalletConsentOwner) retain(ctx context.Context, original protocol.WalletMappingConsent) error {
	journal, previous, previousHash, err := self.read(ctx)
	if err != nil {
		return err
	}
	if len(journal.Records) > 0 {
		last := journal.Records[len(journal.Records)-1]
		if last.Original == original {
			return ctx.Err()
		}
		if !last.Acknowledged {
			return errors.New("wallet consent has an unacknowledged original; replay it before another mapping")
		}
	}
	if len(journal.Records) >= protocol.MaxWalletMappingHistory {
		return protocol.ErrWalletMappingCapacity
	}
	statement, _, err := protocol.VerifyWalletMappingConsent(ctx, original)
	if err != nil {
		return err
	}
	if err := snWalletConsentSuccessor(previous, previousHash, statement); err != nil {
		return err
	}
	journal.Records = append(journal.Records, snWalletConsentRecord{Original: original})
	return self.publish(ctx, journal)
}

// Only the exact current signed hash and generation may complete this handoff.
// A stale response cannot acknowledge a successor, and repetition is harmless.
func (self *snWalletConsentOwner) acknowledge(ctx context.Context, originalHash [32]byte, generation uint64) error {
	journal, statement, hash, err := self.read(ctx)
	if err != nil {
		return err
	}
	if statement == nil || hash != originalHash || statement.Generation != generation {
		return errors.New("wallet consent acknowledgement differs from the retained original")
	}
	last := &journal.Records[len(journal.Records)-1]
	if last.Acknowledged {
		return ctx.Err()
	}
	last.Acknowledged = true
	return self.publish(ctx, journal)
}

// Releasing process custody is part of the caller's completed result. Close is
// idempotent and never removes originals, changes acknowledgement or signs.
func (self *snWalletConsentOwner) close() error {
	if self == nil || self.store == nil {
		if self == nil {
			return nil
		}
		return self.closeErr
	}
	self.closeErr = self.store.close()
	self.store = nil
	return self.closeErr
}

// Check context before touching custody. A changed journal under a live owner
// is refused even when the replacement is another canonical signed inventory.
func (self *snWalletConsentOwner) read(ctx context.Context) (*snWalletConsentJournal, *protocol.WalletMappingStatement, [32]byte, error) {
	if ctx == nil {
		return nil, nil, [32]byte{}, protocol.ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, [32]byte{}, err
	}
	if self == nil || self.store == nil {
		return nil, nil, [32]byte{}, errors.New("wallet consent owner is closed")
	}
	if self.failed != nil {
		return nil, nil, [32]byte{}, self.failed
	}
	raw, err := self.store.read()
	if err != nil {
		self.failed = fmt.Errorf("wallet consent custody is unavailable: %w", err)
		return nil, nil, [32]byte{}, self.failed
	}
	digest := sha256.Sum256(raw)
	if self.hasDigest && digest != self.digest {
		self.failed = errors.New("wallet consent journal changed outside its retained owner")
		return nil, nil, [32]byte{}, self.failed
	}
	journal, statement, hash, err := decodeSnWalletConsent(ctx, raw, self.apiUrl)
	if err != nil {
		if ctx.Err() == nil {
			self.failed = err
		}
		return nil, nil, [32]byte{}, err
	}
	self.digest, self.hasDigest = digest, true
	return journal, statement, hash, nil
}

// File sync, close, atomic rename and directory sync must all succeed before
// this method can authorize its caller's first HTTP submission.
func (self *snWalletConsentOwner) publish(ctx context.Context, journal *snWalletConsentJournal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if len(raw) > snWalletConsentMaxBytes {
		return protocol.ErrWalletMappingCapacity
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := self.store.write(raw, self.digest, self.hasDigest); err != nil {
		self.failed = fmt.Errorf("wallet consent publication is uncertain; no request may be sent: %w", err)
		return self.failed
	}
	self.digest, self.hasDigest = sha256.Sum256(raw), true
	return ctx.Err()
}

// Strict outer JSON admission also rejects changed escaping, array lengths,
// aliases and whitespace. Protocol verification checks each exact signed body.
func decodeSnWalletConsent(ctx context.Context, raw []byte, apiUrl string) (*snWalletConsentJournal, *protocol.WalletMappingStatement, [32]byte, error) {
	if ctx == nil {
		return nil, nil, [32]byte{}, protocol.ErrWalletMappingUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, [32]byte{}, err
	}
	if len(raw) == 0 || len(raw) > snWalletConsentMaxBytes {
		return nil, nil, [32]byte{}, protocol.ErrWalletMappingCapacity
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, nil, [32]byte{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var journal snWalletConsentJournal
	if err := decoder.Decode(&journal); err != nil {
		return nil, nil, [32]byte{}, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, nil, [32]byte{}, errors.New("wallet consent journal has trailing data")
	}
	canonical, err := json.Marshal(journal)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, nil, [32]byte{}, errors.Join(errors.New("wallet consent journal is not canonical"), err)
	}
	if journal.Schema != snWalletConsentSchema || journal.ApiUrl != apiUrl || journal.Records == nil {
		return nil, nil, [32]byte{}, errors.New("wallet consent journal schema or selected API URL differs")
	}
	if len(journal.Records) > protocol.MaxWalletMappingHistory {
		return nil, nil, [32]byte{}, protocol.ErrWalletMappingCapacity
	}
	var previous *protocol.WalletMappingStatement
	var previousHash [32]byte
	for index, record := range journal.Records {
		if index < len(journal.Records)-1 && !record.Acknowledged {
			return nil, nil, [32]byte{}, errors.New("wallet consent journal replaced an unacknowledged original")
		}
		statement, hash, err := protocol.VerifyWalletMappingConsent(ctx, record.Original)
		if err != nil {
			return nil, nil, [32]byte{}, err
		}
		if err := snWalletConsentSuccessor(previous, previousHash, statement); err != nil {
			return nil, nil, [32]byte{}, err
		}
		previous, previousHash = statement, hash
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, [32]byte{}, err
	}
	return &journal, previous, previousHash, nil
}

// External rotations can leave gaps. Adjacent locally retained generations
// still have to name their exact predecessor; every retained identity is fixed.
func snWalletConsentSuccessor(previous *protocol.WalletMappingStatement, previousHash [32]byte, next *protocol.WalletMappingStatement) error {
	if next.Generation > protocol.MaxWalletMappingHistory {
		return protocol.ErrWalletMappingCapacity
	}
	if previous == nil {
		return nil
	}
	if next.Domain != previous.Domain || next.ClientId != previous.ClientId || next.NetworkId != previous.NetworkId || next.UserId != previous.UserId || next.Generation <= previous.Generation || next.FromEpoch <= previous.FromEpoch || next.Generation == previous.Generation+1 && next.PreviousHash != previousHash {
		return protocol.ErrWalletMappingIntegrity
	}
	return nil
}
