// The roster authority consumes an explicitly reviewed complete population.
// Transport discovery supplies originals, never authority to omit an owner.
package payoutroster

import (
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
)

const InputSchema = "urnetwork-payout-roster-input-v1"
const RequestSchema = "urnetwork-payout-roster-request-v1"
const MaxRequestBytes = 64 * 1024 * 1024

// Every SDK lifecycle needs its signed enrollment and independently signed
// registration. An old lifecycle remains explicit after a key rotation.
type OwnerInput struct {
	Enrollment   []byte                         `json:"enrollment"`
	Registration protocol.ClientKeyRegistration `json:"registration"`
}

// The full history pins its final head, including future rotations. Legacy v1
// permits nil unknown history; network- or hotkey-enabled input requires an
// explicit [] to assert reviewed absence before a fallback can be selected.
type ProviderInput struct {
	ClientId       [16]byte                        `json:"client_id"`
	NetworkId      [16]byte                        `json:"network_id"`
	WalletConsents []protocol.WalletMappingConsent `json:"wallet_consents"`
}

// One full network history pins a head shared by its expected providers.
// Its consent message has the independent network signing scope.
type NetworkWalletInput struct {
	NetworkId      [16]byte                        `json:"network_id"`
	WalletConsents []protocol.WalletMappingConsent `json:"wallet_consents"`
}

// One full delegation history pins a head shared by its expected providers;
// its messages have the hotkey delegation signing scope. HotkeyConsents is the
// global chain from generation one through exactly the head that the
// delegation effective at the epoch names, and [] when none is effective.
// Hotkey fallback passes the network consent, so the network's own chain is
// either supplied in network_wallets or asserted absent after review.
type HotkeyDelegationInput struct {
	NetworkId           [16]byte                              `json:"network_id"`
	NetworkWalletAbsent bool                                  `json:"network_wallet_absent"`
	DelegationConsents  []protocol.WalletMappingConsent       `json:"delegation_consents"`
	HotkeyConsents      []protocol.HotkeyWalletMappingConsent `json:"hotkey_consents"`
}

// The authority owner selects every participant, even one with no paid work.
// Complete is an explicit operator assertion, not a database-derived verdict.
type Input struct {
	Schema            string                                  `json:"schema"`
	Complete          bool                                    `json:"complete"`
	Epoch             uint64                                  `json:"epoch"`
	Clock             *payoutartifact.ClosedWorkWindowClock   `json:"clock"`
	Owners            []OwnerInput                            `json:"owners"`
	Providers         []ProviderInput                         `json:"providers"`
	NetworkWallets    []NetworkWalletInput                    `json:"network_wallets,omitempty"`
	HotkeyDelegations []HotkeyDelegationInput                 `json:"hotkey_delegations,omitempty"`
	PriorContracts    []payoutartifact.WholeWorkPriorContract `json:"prior_contracts"`
	WorkSources       []protocol.ProviderWorkSourceAuthority  `json:"work_sources"`
}

// Preparation retains originals alongside the exact unsigned derived roster.
// Signing rederives it before loading a key; a digest pins the complete request.
type Request struct {
	Schema           string                            `json:"schema"`
	Input            Input                             `json:"input"`
	Authority        payoutartifact.WholeWorkAuthority `json:"authority"`
	WalletSelections []WalletSelection                 `json:"wallet_selections,omitempty"`
}

// Review metadata displays the shared settlement selector's exact outcome.
// Consumers independently verify the roster and originals, never this preview.
type WalletSelection struct {
	ClientId    [16]byte                `json:"client_id"`
	NetworkId   [16]byte                `json:"network_id"`
	Wallet      *protocol.EarningWallet `json:"wallet,omitempty"`
	Unavailable bool                    `json:"unavailable,omitempty"`
}

// Outputs contain public identifiers only. A retained acknowledgement means
// the API returned the digest of the exact durably retained signed original.
type Result struct {
	DomainHash      string `json:"domain_hash"`
	Epoch           uint64 `json:"epoch"`
	RequestSha256   string `json:"request_sha256"`
	AuthoritySha256 string `json:"authority_sha256"`
	Published       bool   `json:"published"`
}
