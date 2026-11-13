// `provider fleet init` builds a new canonical fleet manifest from flags, so a
// head-tier operator does not have to hand-write the compact, member-sorted
// JSON that protocol.ParseFleetManifest requires (support inbox 7992). It
// prints exactly what `provider fleet manifest` prints, so the output saved to
// a file feeds back into `fleet manifest/publish/sign --manifest`.
//
// The command reads only local files: no RPC, no API, no network. With
// --client_seed_file it uses only the seed's public key; seeds are never
// printed or written, including in errors.

package miner

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
)

const fleetInitDefaultNetuid = uint64(25)

// fleetInitRandom fills the default fleet id; tests pass their own reader.
var fleetInitRandom io.Reader = rand.Reader

// printFleetManifest is the shared output of `fleet manifest` and `fleet init`.
func printFleetManifest(out io.Writer, manifest *protocol.FleetManifest) error {
	canonical, err := manifest.Canonical()
	if err != nil {
		return err
	}
	hash, err := manifest.CommitmentHash()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "%s\ncommitment_sha256: 0x%x\nmembers: %d\n", canonical, hash, len(manifest.Members))
	return err
}

// parseFleetClientID accepts a 16-byte client id as hex or in the uuid form
// fleetSignUuid prints.
func parseFleetClientID(value string) ([16]byte, error) {
	return parseClientID16(strings.ReplaceAll(value, "-", ""))
}

func parseFleetFixedHex(name, value string, out []byte) error {
	b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
	if err != nil || len(b) != len(out) {
		return fmt.Errorf("%s must be a %d-byte 0x hex value", name, len(out))
	}
	copy(out, b)
	return nil
}

// parseFleetHotkey accepts the hotkey public key as 0x 32-byte hex or as an
// ss58 address (prefix 42).
func parseFleetHotkey(value string) ([32]byte, error) {
	var out [32]byte
	if strings.HasPrefix(value, "0x") {
		err := parseFleetFixedHex("--hotkey", value, out[:])
		return out, err
	}
	key, err := ss58.DecodeWithPrefix(value, ss58.BittensorPrefix)
	if err != nil {
		return out, fmt.Errorf("--hotkey must be a 0x 32-byte hex public key or an ss58 address: %w", err)
	}
	return key, nil
}

// parseFleetInitMember parses one --member=<client_id>:<client_key>.
func parseFleetInitMember(value string) (protocol.FleetMember, error) {
	var member protocol.FleetMember
	clientID, clientKey, ok := strings.Cut(value, ":")
	if !ok {
		return member, fmt.Errorf("--member %q must be <client_id>:<client_key>", value)
	}
	id, err := parseFleetClientID(clientID)
	if err != nil {
		return member, fmt.Errorf("--member %q: client_id must be a 16-byte hex value or uuid", value)
	}
	member.ClientID = id
	if err := parseFleetFixedHex("--member client_key", clientKey, member.ClientKey[:]); err != nil {
		return member, err
	}
	return member, nil
}

// fleetInitManifest builds the manifest and returns it as the strict parser
// reads its canonical form, so the output is exactly what --manifest accepts.
func fleetInitManifest(opts docopt.Opts, random io.Reader) (*protocol.FleetManifest, error) {
	manifest := &protocol.FleetManifest{Schema: protocol.FleetManifestSchema}
	chainID, err := strconv.ParseUint(fleetOpt(opts, "--chain_id"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("--chain_id: %w", err)
	}
	manifest.ChainID = chainID
	netuid, err := fleetUint64Opt(opts, "--netuid", fleetInitDefaultNetuid)
	if err != nil {
		return nil, err
	}
	if netuid > 0xffff {
		return nil, errors.New("--netuid must fit in 16 bits")
	}
	manifest.Netuid = uint16(netuid)
	if manifest.Generation, err = fleetUint64Opt(opts, "--generation", 1); err != nil {
		return nil, err
	}
	if err := parseFleetFixedHex("--coordinator", fleetOpt(opts, "--coordinator"), manifest.Coordinator[:]); err != nil {
		return nil, err
	}
	if manifest.Hotkey, err = parseFleetHotkey(fleetOpt(opts, "--hotkey")); err != nil {
		return nil, err
	}
	if value := fleetOpt(opts, "--fleet_id"); value != "" {
		if err := parseFleetFixedHex("--fleet_id", value, manifest.FleetID[:]); err != nil {
			return nil, err
		}
	} else if _, err := io.ReadFull(random, manifest.FleetID[:]); err != nil {
		return nil, fmt.Errorf("generate fleet_id: %w", err)
	}

	clientIDValue := fleetOpt(opts, "--client_id")
	if seedPath := fleetOpt(opts, "--client_seed_file"); seedPath != "" {
		// The provider client JWT path depends on proxy settings, so the
		// device's client id is given explicitly rather than guessed.
		if clientIDValue == "" {
			return nil, errors.New("--client_seed_file needs --client_id (the device's 16-byte UR client id)")
		}
		clientID, err := parseFleetClientID(clientIDValue)
		if err != nil {
			return nil, err
		}
		clientPrivate, err := loadEd25519Seed(seedPath)
		if err != nil {
			return nil, err
		}
		member := protocol.FleetMember{ClientID: clientID}
		copy(member.ClientKey[:], clientPrivate.Public().(ed25519.PublicKey))
		manifest.Members = append(manifest.Members, member)
	} else if clientIDValue != "" {
		return nil, errors.New("--client_id needs --client_seed_file; use --member=<client_id>:<client_key> for other devices")
	}
	for _, value := range fleetOpts(opts, "--member") {
		member, err := parseFleetInitMember(value)
		if err != nil {
			return nil, err
		}
		manifest.Members = append(manifest.Members, member)
	}
	if len(manifest.Members) == 0 {
		return nil, errors.New("fleet init needs at least one --member, or --client_seed_file with --client_id")
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		return nil, err
	}
	return protocol.ParseFleetManifest(canonical)
}

func fleetInit(opts docopt.Opts, out io.Writer, random io.Reader) error {
	manifest, err := fleetInitManifest(opts, random)
	if err != nil {
		return err
	}
	return printFleetManifest(out, manifest)
}
