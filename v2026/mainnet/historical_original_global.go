// Original-global aliases bind optional host visibility, never replacement
// globals, modified instructions, a caller-frame layout, or new authority.
package main

import (
	"errors"
	"fmt"
	"strings"
)

func (self *historicalReplayObservationProfile) validateOriginalGlobals() error {
	if len(self.OriginalGlobals) > 4 || len(self.OriginalGlobals) != 0 && self.Schema != historicalNativeProfileSchema {
		return errors.New("original global aliases require a bounded native profile")
	}
	declared := map[string]bool{}
	for index, alias := range self.OriginalGlobals {
		if alias.ExportName != fmt.Sprintf("__urnetwork_observe_global_%d", alias.GlobalIndex) || index > 0 && self.OriginalGlobals[index-1].GlobalIndex >= alias.GlobalIndex {
			return errors.New("original global aliases differ from canonical ordered indices")
		}
		declared[alias.ExportName] = false
	}
	mark := func(name *string) error {
		if name == nil || !strings.HasPrefix(*name, "__urnetwork_observe_global_") {
			return nil
		}
		if _, ok := declared[*name]; !ok {
			return errors.New("original capture alias is undeclared")
		}
		declared[*name] = true
		return nil
	}
	for _, rule := range self.Rules {
		for _, capture := range rule.Memory {
			if err := mark(capture.Global); err != nil {
				return err
			}
			if capture.Repeat != nil {
				if err := mark(capture.Repeat.Count.Global); err != nil {
					return err
				}
			}
		}
	}
	for _, used := range declared {
		if !used {
			return errors.New("original global alias has no reviewed capture use")
		}
	}
	return nil
}
