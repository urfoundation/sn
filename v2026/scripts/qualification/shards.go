// Exact root partitions keep queueing outside each test process's deadline.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type shardMembership struct {
	Suite string   `json:"suite"`
	Roots []string `json:"roots"`
}

type suitePartition struct {
	Suite  string            `json:"suite"`
	Roots  []string          `json:"roots"`
	Shards []shardMembership `json:"shards"`
}

// A root and all its declared descendants move together. There is no selection
// based on prior outcomes, timing, or observed events; the union stays exact.
func partitionSuites(plan planSpec, capture string) (planSpec, []suitePartition, error) {
	result := plan
	result.Suites = nil
	var partitions []suitePartition
	for _, suite := range plan.Suites {
		if suite.RootsPerShard == 0 {
			result.Suites = append(result.Suites, suite)
			continue
		}
		if suite.RootsPerShard < 1 || suite.RootsPerShard > 1024 || len(suite.Id) > 57 {
			return planSpec{}, nil, errors.New("invalid shard size or suite id too long for shard suffix")
		}
		expected, err := expectedInputs(suite.Outcomes, suite.FailureLiterals)
		if err != nil {
			return planSpec{}, nil, err
		}
		partition := suitePartition{Suite: suite.Id, Roots: expected.Roots}
		for offset := 0; offset < len(expected.Roots); offset += suite.RootsPerShard {
			if len(result.Suites) >= 1024 {
				return planSpec{}, nil, errors.New("expanded shard count exceeds suite bound")
			}
			roots := expected.Roots[offset:min(offset+suite.RootsPerShard, len(expected.Roots))]
			members := map[string]bool{}
			for _, root := range roots {
				members[root] = true
			}
			shard := suite
			shard.Id = fmt.Sprintf("%s-s%04d", suite.Id, len(partition.Shards)+1)
			shard.RootsPerShard = 0
			shard.Outcomes = filepath.Join(capture, shard.Id+".outcomes.tsv")
			shard.FailureLiterals = filepath.Join(capture, shard.Id+".literals.tsv")
			var outcomes, literals strings.Builder
			for _, identity := range sortedKeys(expected.Outcomes) {
				root, _, _ := strings.Cut(identity, "/")
				if !members[root] {
					continue
				}
				fmt.Fprintf(&outcomes, "%s\t%s\n", identity, strings.ToUpper(expected.Outcomes[identity]))
				if marker := expected.Markers[identity]; marker != "" {
					fmt.Fprintf(&literals, "%s\t%s\n", identity, marker)
				}
			}
			for path, data := range map[string]string{shard.Outcomes: outcomes.String(), shard.FailureLiterals: literals.String()} {
				file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					return planSpec{}, nil, err
				}
				_, writeErr := file.WriteString(data)
				if err := errors.Join(writeErr, file.Close()); err != nil {
					return planSpec{}, nil, err
				}
			}
			partition.Shards = append(partition.Shards, shardMembership{Suite: shard.Id, Roots: roots})
			result.Suites = append(result.Suites, shard)
		}
		partitions = append(partitions, partition)
	}
	if err := validatePlan(result); err != nil {
		return planSpec{}, nil, err
	}
	return result, partitions, nil
}
