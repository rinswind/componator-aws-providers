// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// Package sources provides a plugin architecture for Helm chart sources.
// This package defines the ChartSourceFactory and ChartSource interfaces that all source types implement,
// along with a Registry for factory instance management.
//
// Design: Factory pattern for thread-safety
//   - ChartSourceFactory: Stateless singletons stored in registry
//   - ChartSource: Immutable per-reconciliation instances created by factories
//
// This eliminates race conditions by avoiding shared mutable state between reconciliations.
package sources

import (
	"context"
	"encoding/json"

	"helm.sh/helm/v3/pkg/cli"
)

// ChartSourceFactory creates ChartSource instances with validated configuration.
// Factories are stateless singletons that parse and validate configuration,
// then create immutable ChartSource instances for per-reconciliation use.
//
// Lifecycle:
//   - Created once: Factory instances are singletons initialized in NewComponentReconciler
//   - Thread-safe: No mutable state, safe for concurrent use
//   - Creates sources: Called per reconciliation to create configured ChartSource instances
//
// This design enables thread-safe concurrent reconciliation by eliminating shared mutable state.
type ChartSourceFactory interface {
	// Type returns the source type identifier ("http" or "oci").
	// This is used for factory registration and lookup in the registry.
	Type() string

	// CreateSource parses, validates configuration, and creates an immutable ChartSource instance.
	// This is called per reconciliation with fresh config from Component.Spec.Config.
	//
	// The factory should:
	//   - Extract the "source" section from rawConfig
	//   - Unmarshal into its configuration struct
	//   - Validate all required fields
	//   - Create a ChartSource instance with immutable configuration
	//   - Return error if parsing or validation fails
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - rawConfig: Raw JSON from Component.Spec.Config (the entire config, not just source section)
	//   - settings: Helm CLI settings for chart operations
	//
	// Returns a configured ChartSource instance, or error if creation fails.
	CreateSource(ctx context.Context, rawConfig json.RawMessage, settings *cli.EnvSettings) (ChartSource, error)
}

// ChartSource provides a plugin interface for retrieving Helm charts from different sources.
// Implementations include HTTP (traditional Helm repositories) and OCI (registry support).
//
// Lifecycle:
//   - Created per reconciliation: Factory creates instance with immutable configuration
//   - Thread-safe: Immutable after creation, safe for use within single reconciliation
//   - Used for chart retrieval: LocateChart called with configuration baked in
//
// This design eliminates type-switching and enables easy addition of new source types
// (git, S3, local) without modifying core controller logic.
type ChartSource interface {
	// LocateChart retrieves a Helm chart and returns the path to the cached chart file.
	// The source was created with validated configuration, no additional setup needed.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//
	// Returns the path to the downloaded chart file (typically a .tgz in Helm cache),
	// or error if retrieval fails.
	LocateChart(ctx context.Context) (string, error)

	// GetVersion returns the configured chart version.
	// This is used for status reporting and change detection.
	//
	// Returns the chart version, or empty string if not available.
	GetVersion() string
}
