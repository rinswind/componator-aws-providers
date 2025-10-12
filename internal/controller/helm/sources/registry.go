// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"fmt"
)

// Registry is a simple lookup table mapping source type → source instance.
// This enables source discovery and retrieval without type-switching logic.
//
// Design: Plain map created once during controller initialization.
// No mutex needed since all registration happens before concurrent access begins.
type Registry map[string]ChartSource

// NewRegistry creates a new empty source registry.
func NewRegistry() Registry {
	return make(Registry)
}

// Get retrieves a registered source instance by type.
//
// Parameters:
//   - sourceType: Source type identifier ("http", "oci", etc.)
//
// Returns the registered source instance, or error if the type is unknown.
func (r Registry) Get(sourceType string) (ChartSource, error) {
	source, found := r[sourceType]
	if !found {
		return nil, fmt.Errorf("unknown source type: %s (available types: %v)", sourceType, r.availableTypes())
	}

	return source, nil
}

// availableTypes returns a slice of registered source types for error messages.
func (r Registry) availableTypes() []string {
	types := make([]string, 0, len(r))
	for t := range r {
		types = append(types, t)
	}
	return types
}
