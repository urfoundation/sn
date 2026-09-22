// Typed metadata rows have fixed Json framing. Count their exact encoded
// widths before the corpus allocation without encoding every row twice.
package main

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// Standard-library prototypes own field names, indentation and omission
// framing. Only varying string/integer/base64 widths are counted directly.
type campaignMetadataRowSizerV2 struct {
	maximumBytes uint64
	source       uint64
	sourceOrigin uint64
	carrier      uint64
}

// The three small prototypes are per validation call, never a global cache.
// Source Origin is the only optional field in these two concrete row types.
func newCampaignMetadataRowSizerV2(maximumBytes uint64) (*campaignMetadataRowSizerV2, error) {
	if maximumBytes == 0 || maximumBytes > maximumCampaignMetadataDocumentCapacity {
		return nil, errors.New("metadata row size has no finite document owner")
	}
	row := FinalCollectedValidatorSourceV2{}
	source, err := json.MarshalIndent(row, "          ", "  ")
	if err != nil {
		return nil, err
	}
	row.Source.Origin = "x"
	sourceOrigin, err := json.MarshalIndent(row, "          ", "  ")
	if err != nil {
		return nil, err
	}
	carrier, err := json.MarshalIndent(FinalCollectedPriorCarrierV2{}, "          ", "  ")
	if err != nil {
		return nil, err
	}
	return &campaignMetadataRowSizerV2{maximumBytes: maximumBytes, source: uint64(len(source)) + 12, sourceOrigin: uint64(len(sourceOrigin)) + 12 - 1, carrier: uint64(len(carrier)) + 12}, nil
}

// Quotes already belong to the prototype. Match encoding/json's Html-safe
// content width, including replacement of each malformed Utf8 byte. A width
// above the independent document ceiling is represented by one over that cap.
func campaignMetadataStringContentBytesV2(value string, maximumBytes uint64) uint64 {
	size := uint64(len(value))
	if size > maximumBytes {
		return maximumBytes + 1
	}
	for index := 0; index < len(value); {
		c := value[index]
		if c < utf8.RuneSelf {
			switch c {
			case '"', '\\', '\b', '\f', '\n', '\r', '\t':
				size++
			case '<', '>', '&':
				size += 5
			default:
				if c < 0x20 {
					size += 5
				}
			}
			index++
		} else {
			runeValue, width := utf8.DecodeRuneInString(value[index:])
			if runeValue == utf8.RuneError && width == 1 {
				size += 5
			} else if runeValue == '\u2028' || runeValue == '\u2029' {
				size += 3
			}
			index += width
		}
		if size > maximumBytes {
			return maximumBytes + 1
		}
	}
	return size
}

// Zero contributes the prototype's one digit; uint64 has at most twenty.
func campaignMetadataUintExtraBytesV2(value uint64) uint64 {
	var extra uint64
	for value >= 10 {
		value /= 10
		extra++
	}
	return extra
}

// Every source leaf keeps its encoded width; empty Origin keeps the original
// omitted-field shape instead of charging a present empty field.
func (self *campaignMetadataRowSizerV2) sourceBytes(row FinalCollectedValidatorSourceV2) uint64 {
	size := self.source
	if row.Source.Origin != "" {
		size = self.sourceOrigin
	}
	for _, value := range []string{row.Source.Kind, row.Source.Name, row.Source.Origin, row.Artifact.Kind, row.Artifact.URI, row.Artifact.ContentHash} {
		size += campaignMetadataStringContentBytesV2(value, self.maximumBytes)
		if size > self.maximumBytes {
			return self.maximumBytes + 1
		}
	}
	return min(size+campaignMetadataUintExtraBytesV2(row.Artifact.SizeBytes), self.maximumBytes+1)
}

// Nil suffix encodes null; non-nil empty suffix encodes an empty string.
// Arbitrary bytes retain the exact standard base64 width, not a suffix guess.
func (self *campaignMetadataRowSizerV2) carrierBytes(row FinalCollectedPriorCarrierV2) uint64 {
	size := self.carrier
	for _, value := range []string{row.Scope, row.Path, row.EnvelopeHash, row.WireHash, row.LocalHash} {
		size += campaignMetadataStringContentBytesV2(value, self.maximumBytes)
		if size > self.maximumBytes {
			return self.maximumBytes + 1
		}
	}
	if row.LocalSuffix != nil {
		if uint64(len(row.LocalSuffix)) > self.maximumBytes {
			return self.maximumBytes + 1
		}
		size = size - 2 + 4*((uint64(len(row.LocalSuffix))+2)/3)
	}
	return min(size+campaignMetadataUintExtraBytesV2(row.WireBytes)+campaignMetadataUintExtraBytesV2(row.LocalBytes), self.maximumBytes+1)
}
