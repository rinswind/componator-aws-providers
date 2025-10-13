// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package composite

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources"
	"helm.sh/helm/v3/pkg/cli"
)

// Registry is a composite ChartSourceFactory that delegates to type-specific factories.
// It implements the Composite Pattern: Registry itself implements ChartSourceFactory
// and internally routes to the appropriate concrete factory based on detected source type.
//
// Design: Plain map created once during controller initialization.
// No mutex needed since all registration happens before concurrent access begins.
//
// This encapsulates the detect→lookup→create flow, simplifying caller code from:
//
//	sourceType := DetectSourceType(rawConfig)
//	factory := registry.Get(sourceType)
//	source := factory.CreateSource(...)
//
// To:
//
//	source := registry.CreateSource(...)
type Registry map[string]sources.ChartSourceFactory

// NewFactory creates a new empty factory registry.
func NewFactory() Registry {
	return make(Registry)
}

// Register adds a factory to the registry using its Type() as the key.
//
// Parameters:
//   - factory: ChartSourceFactory instance to register
func (r Registry) Register(factory sources.ChartSourceFactory) {
	r[factory.Type()] = factory
}

// Type returns the source type identifier for the composite registry.
// This is not typically used since Registry acts as a meta-factory.
func (r Registry) Type() string {
	return "composite"
}

// CreateSource implements ChartSourceFactory by detecting the source type
// and delegating to the appropriate registered factory.
//
// This implements the Composite Pattern by:
//  1. Detecting source type from rawConfig (which is the source section)
//  2. Looking up the appropriate factory
//  3. Delegating to that factory's CreateSource
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - rawConfig: Raw JSON of the source section (not the full Component.Spec.Config)
//   - settings: Helm CLI settings for chart operations
//
// Returns a configured ChartSource instance, or error if:
//   - Source type detection fails
//   - Source type is not registered
//   - Underlying factory's CreateSource fails
func (r Registry) CreateSource(
	ctx context.Context, rawConfig json.RawMessage, settings *cli.EnvSettings) (sources.ChartSource, error) {

	// Step 1: Detect source type from the source section
	sourceType, err := detectSourceType(rawConfig)
	if err != nil {
		return nil, err
	}

	// Step 2: Lookup factory
	factory, found := r[sourceType]
	if !found {
		return nil, fmt.Errorf("unknown source type: %s (available types: %v)", sourceType, slices.Collect(maps.Keys(r)))
	}

	// Step 3: Delegate to concrete factory
	return factory.CreateSource(ctx, rawConfig, settings)
}

// Get retrieves a registered factory instance by type.
// This method is primarily used for testing and backwards compatibility.
// Most callers should use CreateSource which implements the composite pattern.
//
// Parameters:
//   - sourceType: Source type identifier ("http", "oci", etc.)
//
// Returns the registered factory instance, or error if the type is unknown.
func (r Registry) Get(sourceType string) (sources.ChartSourceFactory, error) {
	factory, found := r[sourceType]
	if !found {
		return nil, fmt.Errorf("unknown source type: %s (available types: %v)", sourceType, slices.Collect(maps.Keys(r)))
	}

	return factory, nil
}

// detectSourceType extracts the source type field from the source section.
//
// Parameters:
//   - rawConfig: Raw JSON of the source section
//
// Returns:
//   - sourceType: The source type identifier ("http", "oci", etc.)
//   - error: Error if parsing fails or type is missing/empty
func detectSourceType(rawConfig json.RawMessage) (string, error) {
	// Parse the type field from source section
	var typeDetector struct {
		Type string `json:"type"`
	}

	if err := json.Unmarshal(rawConfig, &typeDetector); err != nil {
		return "", fmt.Errorf("failed to parse source section: %w", err)
	}

	if typeDetector.Type == "" {
		return "", fmt.Errorf("source.type is required (must be 'http', 'oci', etc.)")
	}

	return typeDetector.Type, nil
}
