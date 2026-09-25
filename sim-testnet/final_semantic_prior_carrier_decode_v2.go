// Exact prior-wire verification borrows its already-owned payload and streams
// the producer's base64 source into a bounded hash owner. No authenticated
// object or borrowed bytes survive the synchronous verification call.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Accepted prior carriers already require exact canonical publisher framing.
// These boundaries only split borrowed bytes: the caller still validates the
// payload, recovers its signature, checks its typed original and compares
// every canonical wire byte before accepting this untrusted input.
func decodeFinalPriorCarrierEnvelopeV2(wire []byte) (ReleaseEvidenceEnvelope, error) {
	var envelope ReleaseEvidenceEnvelope
	payloadStart := bytes.Index(wire, []byte(`,"payload":`))
	payloadEnd := bytes.LastIndex(wire, []byte(`,"signer":`))
	if payloadStart < 0 || payloadEnd <= payloadStart+len(`,"payload":`) {
		return envelope, errors.New("prior carrier canonical envelope framing is absent")
	}
	payloadStart += len(`,"payload":`)
	if err := decodeFinalPriorCarrierMetadataV2(wire[:payloadStart], wire[payloadEnd:], &envelope); err != nil {
		return envelope, err
	}
	envelope.Payload = wire[payloadStart:payloadEnd]
	return envelope, nil
}

// Replacing only a borrowed value with null keeps the actual destination
// schema, unknown-field rejection and trailing-value rule, without forcing
// Decoder to buffer another copy of the complete multi-gigabyte value.
func decodeFinalPriorCarrierMetadataV2(prefix, suffix []byte, value any) error {
	decoder := json.NewDecoder(io.MultiReader(bytes.NewReader(prefix), strings.NewReader("null"), bytes.NewReader(suffix)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("prior carrier metadata has a trailing value")
		}
		return err
	}
	return nil
}

// Hash-addressed intent controls retain the original decoded schema check.
// Other originals need only their exact typed metadata, byte count and hash.
// Historical escaped strings, arrays and reordered fields use the unchanged
// strict decoder; the producer's final plain base64 string needs no raw copy.
// Call only after this invocation's fresh payload grammar/signature check.
func verifyFinalPriorCarrierFilePayloadV2(limits campaignEvidenceLimits, runId, scope string, entry campaignEvidenceFileEntry, encoded []byte) error {
	var payload campaignEvidenceFilePayload
	var base64Source []byte
	if !campaignMetadataSourcePathV2(entry.Path) && !finalJournalCarrierRequiresBodyV2(entry.Path) {
		dataStart := bytes.LastIndex(encoded, []byte(`,"data":"`))
		if dataStart >= 0 && dataStart+len(`,"data":"`) <= len(encoded)-2 && bytes.HasSuffix(encoded, []byte(`"}`)) {
			dataStart += len(`,"data":"`)
			candidate := encoded[dataStart : len(encoded)-2]
			if !bytes.ContainsRune(candidate, '\\') && !bytes.ContainsRune(candidate, '"') {
				if err := decodeFinalPriorCarrierMetadataV2(encoded[:dataStart-1], encoded[len(encoded)-1:], &payload); err != nil {
					return err
				}
				base64Source = candidate
			}
		}
	}
	if base64Source == nil {
		if err := decodeStrictJSONBytes(encoded, &payload); err != nil {
			return err
		}
	}
	if payload.Schema != campaignEvidenceFileSchema || payload.RunID != runId || payload.Scope != scope || payload.Path != entry.Path || payload.ContentHash != entry.ContentHash || payload.Size != entry.Size {
		return errors.New("prior carrier does not contain its exact manifest-named original source")
	}
	if err := validateCampaignMetadataRawSizeV2(limits, entry.Path, payload.Size); err != nil {
		return err
	}
	if base64Source == nil {
		if uint64(len(payload.Data)) != entry.Size || bytesSHA256(payload.Data) != entry.ContentHash {
			return errors.New("prior carrier does not contain its exact manifest-named original source")
		}
		return validateCampaignMetadataRawV2(limits, entry.Path, payload.Data)
	}
	hash := sha256.New()
	const encodedChunkBytes = 64 * 1024
	decoded := make([]byte, encodedChunkBytes/4*3)
	var count uint64
	for len(base64Source) != 0 {
		chunkBytes := min(len(base64Source), encodedChunkBytes)
		n, err := base64.StdEncoding.Decode(decoded, base64Source[:chunkBytes])
		if err != nil {
			return err
		}
		// Padding can end only the complete string, never an intermediate
		// chunk. Keep StdEncoding's historical non-strict padding-bit rule.
		if chunkBytes < len(base64Source) && n != chunkBytes/4*3 {
			return errors.New("prior carrier base64 has data after padding")
		}
		if uint64(n) > entry.Size-count {
			return errors.New("prior carrier exceeds its exact original byte count")
		}
		_, _ = hash.Write(decoded[:n])
		count += uint64(n)
		base64Source = base64Source[chunkBytes:]
	}
	if count != entry.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != entry.ContentHash {
		return errors.New("prior carrier does not contain its exact manifest-named original source")
	}
	return nil
}
