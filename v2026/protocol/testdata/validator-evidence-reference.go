package main

// Public cross-language vectors use literal hexadecimal field widths/order instead
// of any protocol package or candidate payload/digest implementation. Fixed
// synthetic seeds are test material, not wallet keys. Output is stdout only.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vedhavyas/go-subkey/v2/sr25519"
)

// Decodes a reference field list without Go struct packing or ABI helpers.
func fields(hexFields ...string) []byte {
	data, err := hex.DecodeString(strings.Join(hexFields, ""))
	if err != nil {
		panic(err)
	}
	return data
}

// Emits immutable public vectors for both supported evidence kinds.
func main() {
	var vpkSeed, hotkeySeed [32]byte
	for index := range vpkSeed {
		vpkSeed[index], hotkeySeed[index] = byte(index), byte(0x80+index)
	}
	vpkKey := ed25519.NewKeyFromSeed(vpkSeed[:])
	hotkeyKey, err := (sr25519.Scheme{}).FromSeed(hotkeySeed[:])
	if err != nil {
		panic(err)
	}
	vpkHex := hex.EncodeToString(vpkKey.Public().(ed25519.PublicKey))
	hotkeyHex := hex.EncodeToString(hotkeyKey.Public())
	domainHex := strings.Join([]string{
		"00000000000003b1", // chain id 945, uint64
		strings.Repeat("13", 32),
		"0011", // netuid 17, uint16
		strings.Repeat("11", 20),
		strings.Repeat("12", 20),
		strings.Repeat("14", 32),
		strings.Repeat("15", 32),
		"000000000000002a", // activation epoch 42, uint64
		strings.Repeat("16", 32),
	}, "")
	slotDomainHex := strings.Join([]string{
		"00000000000003b1", strings.Repeat("13", 32), "0011",
		strings.Repeat("11", 20), strings.Repeat("12", 20), strings.Repeat("14", 32),
	}, "")
	vectorKVs := map[string]map[string]string{}
	for _, vector := range []struct {
		name         string
		kindHex      string
		boundaryHex  string
		payloadBytes string
		observation  string
		nativeEpoch  string
	}{
		{name: "closed", kindHex: "01", boundaryHex: "0000000000000423", payloadBytes: "0000000000001000"},
		{name: "audit", kindHex: "02", boundaryHex: "000000000000042e", payloadBytes: "0000000000000200", observation: "000000000000002d", nativeEpoch: "00000000000002bc"},
	} {
		var subjectHash [32]byte
		if vector.name == "audit" {
			subjectHash = sha256.Sum256(fields(
				hex.EncodeToString([]byte("urnetwork/validator-evidence-audit-subject/v1")), "00",
				vector.observation, vector.nativeEpoch,
			))
		}
		subjectHex := hex.EncodeToString(subjectHash[:])
		payload := fields(
			hex.EncodeToString([]byte("urnetwork/validator-evidence-header/v1")), "00", domainHex,
			hotkeyHex, "0000000000000007", "000000000000002c", vector.kindHex, subjectHex, vpkHex,
			vector.boundaryHex, strings.Repeat("17", 32), strings.Repeat("18", 32),
			strings.Repeat("19", 32), vector.payloadBytes,
		)
		digest := sha256.Sum256(payload)
		slot := sha256.Sum256(fields(
			hex.EncodeToString([]byte("urnetwork/validator-evidence-slot/v1")), "00", slotDomainHex,
			hotkeyHex, "0000000000000007", "000000000000002c", vector.kindHex, subjectHex,
		))
		vpkSignature := ed25519.Sign(vpkKey, digest[:])
		hotkeySignature, err := hotkeyKey.Sign(digest[:])
		if err != nil {
			panic(err)
		}
		if !ed25519.Verify(vpkKey.Public().(ed25519.PublicKey), digest[:], vpkSignature) || !hotkeyKey.Verify(digest[:], hotkeySignature) {
			panic("reference vector failed signature self-check")
		}
		vectorKVs[vector.name] = map[string]string{
			"payload": hex.EncodeToString(payload), "digest": hex.EncodeToString(digest[:]),
			"slot_key": hex.EncodeToString(slot[:]), "subject_hash": subjectHex,
			"hotkey": hotkeyHex, "vpk": vpkHex,
			"vpk_signature": hex.EncodeToString(vpkSignature), "hotkey_signature": hex.EncodeToString(hotkeySignature),
		}
	}
	encoded, err := json.MarshalIndent(vectorKVs, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(encoded))
}
