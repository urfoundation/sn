// Exact carrier sizing preserves the original closed-bundle wire while making
// its finite chunk ceiling agree with the public campaign archive reader.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Half-open offsets avoid retaining duplicate slices of the source bytes.
type finalCollectedBundleRange struct {
	first int
	end   int
}

// Length arithmetic is checked before a caller allocates encoded source data.
func finalCollectedBase64Bytes(raw uint64) (uint64, error) {
	if raw > ^uint64(0)-2 {
		return 0, errors.New("closed bundle base64 length overflows")
	}
	groups := (raw + 2) / 3
	if groups > ^uint64(0)/4 {
		return 0, errors.New("closed bundle base64 length overflows")
	}
	return groups * 4, nil
}

// Marshal only metadata, not the byte body. Json strings may expand sixfold;
// a byte slice uses base64 and cannot be estimated from raw length alone.
func finalCollectedEntryEncodedBytes(entry FinalCollectedFileBundleEntry) (uint64, error) {
	if entry.SizeBytes != uint64(len(entry.Data)) {
		return 0, errors.New("closed bundle entry size differs")
	}
	metadata := entry
	metadata.Data = nil
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return 0, err
	}
	if entry.Data == nil {
		return uint64(len(encoded)), nil
	}
	dataBytes, err := finalCollectedBase64Bytes(entry.SizeBytes)
	if err != nil {
		return 0, err
	}
	overhead := uint64(len(encoded)) - 2 // Replace null with the two string quotes.
	if dataBytes > ^uint64(0)-overhead {
		return 0, errors.New("closed bundle entry length overflows")
	}
	return dataBytes + overhead, nil
}

// Exact empty-array carrier size includes escaped name/schema bytes.
func finalCollectedBundleOverhead(name string) (uint64, error) {
	if name == "" || strings.ContainsAny(name, "/\\\r\n\x00") {
		return 0, errors.New("closed bundle name is invalid")
	}
	encoded, err := json.Marshal(FinalCollectedFileBundle{Schema: finalCollectedFileBundleSchema, Name: name, Files: []FinalCollectedFileBundleEntry{}})
	return uint64(len(encoded)), err
}

// The largest possible suffix reserves space before the final chunk count is
// known. Each supplied entry is routed once; order/duplicates cannot hide at
// a chunk boundary. The explicit limit also permits tiny deterministic tests.
func finalCollectedBundleChunkRanges(ctx context.Context, name string, entries []FinalCollectedFileBundleEntry, maximum uint64) ([]finalCollectedBundleRange, error) {
	if ctx == nil || len(entries) == 0 || maximum == 0 || maximum > finalPlanBundleBytes(name) {
		return nil, errors.New("closed bundle capacity owner is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := finalCollectedBundleOverhead(name); err != nil {
		return nil, err
	}
	reserveName := fmt.Sprintf("%s-%03d-of-%03d", name, len(entries), len(entries))
	if len(entries) == 1 {
		reserveName = name
	}
	overhead, err := finalCollectedBundleOverhead(reserveName)
	if err != nil || overhead >= maximum {
		return nil, errors.Join(errors.New("closed bundle carrier exceeds its ceiling"), err)
	}
	var ranges []finalCollectedBundleRange
	first, used := 0, overhead
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(entry.Path)))
		if entry.Path == "" || clean != entry.Path || filepath.IsAbs(filepath.FromSlash(entry.Path)) || strings.HasPrefix(entry.Path, "../") || index > 0 && entry.Path <= entries[index-1].Path {
			return nil, errors.New("closed bundle source census is unsafe, duplicated or unordered")
		}
		if uint64(len(entry.Data)) > finalPlanBundleSourceBytes(name, entry.Path) {
			return nil, errors.New("closed bundle source exceeds its raw-file bound")
		}
		entryBytes, err := finalCollectedEntryEncodedBytes(entry)
		if err != nil {
			return nil, err
		}
		if entryBytes > maximum-overhead {
			return nil, fmt.Errorf("captured file %s exceeds encoded bundle ceiling", entry.Path)
		}
		separator := uint64(0)
		if index > first {
			separator = 1
		}
		if entryBytes > maximum-used || separator > maximum-used-entryBytes {
			ranges = append(ranges, finalCollectedBundleRange{first: first, end: index})
			first, used, separator = index, overhead, 0
		}
		used += entryBytes + separator
	}
	ranges = append(ranges, finalCollectedBundleRange{first: first, end: len(entries)})
	return ranges, ctx.Err()
}
