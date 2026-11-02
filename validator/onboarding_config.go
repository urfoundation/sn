// Setup commands authenticate the same production documents as the producer,
// admitting only the existing explicitly unrendered activation census early.
package validator

import "slices"

// Selects the existing loader by document shape, using one immutable read.
// It never admits unsigned production input or relaxes the producer loader.
func loadReleaseBootstrapCommandConfig(path string) (*ReleaseConfig, error) {
	abs, raw, err := readReleaseConfigFile(path)
	if err != nil {
		return nil, err
	}
	document, err := decodeReleaseConfigDocument(abs, raw)
	if err != nil {
		return nil, err
	}
	mode := releaseConfigLoadMode{preActivation: true}
	if document.SchemaVersion == ReleaseMainnetProductionSchemaVersion {
		mode = releaseConfigLoadMode{}
		if slices.ContainsFunc(document.EvidenceV2.Operators, ReleaseEvidenceV2OperatorConfig.Unrendered) {
			mode.productionPreActivation = true
		}
	}
	return decodeReleaseConfigBytesMode(abs, raw, mode)
}
