// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"encoding/json"
	"fmt"
)

// DetectSourceType extracts the source type field from raw configuration.
// This implements the first stage of two-stage parsing for polymorphic source configuration.
//
// Stage 1 (this function): Parse type field to determine source type
// Stage 2 (source's ParseAndValidate): Parse full schema based on detected type
//
// Parameters:
//   - rawConfig: Raw JSON from Component.Spec.Config
//
// Returns the source type identifier ("http", "oci", etc.) or error if:
//   - JSON parsing fails
//   - "source" field is missing
//   - "source.type" field is missing or empty
//
// Example config structure:
//
//	{
//	  "releaseName": "my-release",
//	  "releaseNamespace": "default",
//	  "source": {
//	    "type": "http",
//	    ...
//	  }
//	}
func DetectSourceType(rawConfig json.RawMessage) (string, error) {
	// Parse the raw config to extract the source section
	var configMap map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &configMap); err != nil {
		return "", fmt.Errorf("failed to parse config: %w", err)
	}

	// Ensure source field exists
	rawSource, hasSource := configMap["source"]
	if !hasSource {
		return "", fmt.Errorf("config validation failed: source field is required")
	}

	// Parse the type field from source section
	var typeDetector struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal(rawSource, &typeDetector); err != nil {
		return "", fmt.Errorf("failed to parse source section: %w", err)
	}

	if typeDetector.Type == "" {
		return "", fmt.Errorf("source.type is required (must be 'http', 'oci', etc.)")
	}

	return typeDetector.Type, nil
}
