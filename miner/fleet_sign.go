// `provider fleet sign` makes a device's half (and optionally the hotkey's
// half) of a fleet binding without submitting it. It builds the same binding
// `provider fleet bind` builds from the manifest, signs it locally with the
// device client key and, when given, the fleet hotkey, and prints the payload
// the ur.io Top 200 page takes (POST /sn/head/binding). The operator then
// submits the calldata the page assembles from their own EVM wallet.
//
// The command reads only local files: no RPC, no relayer key, no network. It
// prints public values only (the binding, signatures, digest); seeds and
// private keys are never printed or written, including in errors.

package miner

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/protocol"
)

// fleetSignBinding is the wire form of protocol.FleetBinding that the server's
// SnFleetBinding (and the sdk's binding parser) accept: numbers as json
// numbers, 20/32-byte values as 0x hex, the client id as its uuid.
type fleetSignBinding struct {
	ChainId        uint64 `json:"chain_id"`
	Netuid         uint16 `json:"netuid"`
	Coordinator    string `json:"coordinator"`
	FleetId        string `json:"fleet_id"`
	Hotkey         string `json:"hotkey"`
	ClientId       string `json:"client_id"`
	ClientKey      string `json:"client_key"`
	Generation     uint64 `json:"generation"`
	ValidFromEpoch uint64 `json:"valid_from_epoch"`
	ValidToEpoch   uint64 `json:"valid_to_epoch"`
	CommitmentHash string `json:"commitment_hash"`
}

// fleetSignPayload is the Top 200 page's paste format. hotkey_signature is
// optional there; when present the page uses it for this device.
type fleetSignPayload struct {
	Binding         fleetSignBinding `json:"binding"`
	ClientSignature string           `json:"client_signature"`
	HotkeySignature string           `json:"hotkey_signature,omitempty"`
}

func fleetSignHex(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

// fleetSignUuid formats a 16-byte client id the way server.Id marshals it.
func fleetSignUuid(id [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

func newFleetSignBinding(binding protocol.FleetBinding) fleetSignBinding {
	return fleetSignBinding{
		ChainId:        binding.ChainID,
		Netuid:         binding.Netuid,
		Coordinator:    fleetSignHex(binding.Coordinator[:]),
		FleetId:        fleetSignHex(binding.FleetID[:]),
		Hotkey:         fleetSignHex(binding.Hotkey[:]),
		ClientId:       fleetSignUuid(binding.ClientID),
		ClientKey:      fleetSignHex(binding.ClientKey[:]),
		Generation:     binding.Generation,
		ValidFromEpoch: binding.ValidFromEpoch,
		ValidToEpoch:   binding.ValidToEpoch,
		CommitmentHash: fleetSignHex(binding.CommitmentHash[:]),
	}
}

// fleetSignClientId selects the manifest member: --client_id when given,
// otherwise the one member whose client key is the seed's public key.
func fleetSignClientId(opts docopt.Opts, manifest *protocol.FleetManifest, clientPrivate ed25519.PrivateKey) ([16]byte, error) {
	if value := fleetOpt(opts, "--client_id"); value != "" {
		return parseClientID16(value)
	}
	if clientPrivate == nil {
		return [16]byte{}, errors.New("--client_id is required without --client_seed_file")
	}
	publicKey := clientPrivate.Public().(ed25519.PublicKey)
	for _, member := range manifest.Members {
		if string(member.ClientKey[:]) == string(publicKey) {
			return member.ClientID, nil
		}
	}
	return [16]byte{}, fmt.Errorf("no manifest member has the client key 0x%x; pass --client_id", []byte(publicKey))
}

// fleetSign implements `provider fleet sign`. With --client_seed_file, stdout
// is one line of payload json to paste on the Top 200 page. With only
// --hotkey_seed_file, stdout is the bare 0x hotkey signature for the page's
// hotkey signature field. The digest and labels go to stderr.
func fleetSign(opts docopt.Opts, manifest *protocol.FleetManifest, stdout io.Writer, stderr io.Writer) error {
	clientSeedPath := fleetOpt(opts, "--client_seed_file")
	hotkeySeedPath := fleetOpt(opts, "--hotkey_seed_file")
	if clientSeedPath == "" && hotkeySeedPath == "" {
		return errors.New("fleet sign needs --client_seed_file, --hotkey_seed_file or both")
	}
	var clientPrivate ed25519.PrivateKey
	if clientSeedPath != "" {
		var err error
		clientPrivate, err = loadEd25519Seed(clientSeedPath)
		if err != nil {
			return err
		}
	}
	clientId, err := fleetSignClientId(opts, manifest, clientPrivate)
	if err != nil {
		return err
	}
	binding, err := fleetBindingForClient(opts, manifest, clientId)
	if err != nil {
		return err
	}
	digest, err := binding.Digest()
	if err != nil {
		return err
	}
	payload := fleetSignPayload{Binding: newFleetSignBinding(binding)}
	if clientPrivate != nil {
		clientSignature, err := binding.SignClient(clientPrivate)
		if err != nil {
			return err
		}
		if !binding.VerifyClient(clientSignature) {
			return errors.New("client signature does not verify")
		}
		payload.ClientSignature = fleetSignHex(clientSignature)
	}
	if hotkeySeedPath != "" {
		hotkeySignature, err := fleetSignHotkey(hotkeySeedPath, manifest, binding)
		if err != nil {
			return err
		}
		if !binding.VerifyHotkey(hotkeySignature) {
			return errors.New("hotkey signature does not verify")
		}
		payload.HotkeySignature = fleetSignHex(hotkeySignature)
	}

	fmt.Fprintf(stderr, "binding digest: %s\n", fleetSignHex(digest[:]))
	if clientPrivate == nil {
		fmt.Fprintln(stderr, "hotkey signature for the Top 200 page's hotkey signature field:")
		_, err = fmt.Fprintln(stdout, payload.HotkeySignature)
		return err
	}
	if payload.HotkeySignature != "" {
		fmt.Fprintf(stderr, "hotkey signature (already in the payload): %s\n", payload.HotkeySignature)
	} else {
		fmt.Fprintln(stderr, "no --hotkey_seed_file: sign the digest with the hotkey (provider fleet sign --hotkey_seed_file) for the page's hotkey signature field")
	}
	fmt.Fprintln(stderr, "paste this payload on the Top 200 page:")
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(b))
	return err
}
