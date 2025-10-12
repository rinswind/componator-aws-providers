// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// Package sources provides a plugin architecture for Helm chart sources.
// This package defines the ChartSource interface that all source types implement,
// along with a Registry for source instance management.
//
// Design: Sources are long-lived singletons created once in the controller.
// Each source receives fresh configuration per reconciliation via ParseAndValidate.
package sources

import (
	"context"
	"encoding/json"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
)

// ChartSource provides a plugin interface for retrieving Helm charts from different sources.
// Implementations include HTTP (traditional Helm repositories) and OCI (registry support).
//
// Lifecycle:
//   - Created once: Source instances are singletons initialized in NewComponentReconciler
//   - Configured per reconciliation: ParseAndValidate called with fresh config from Component spec
//   - Used for chart retrieval: GetChart called with validated configuration
//
// This design eliminates type-switching and enables easy addition of new source types
// (git, S3, local) without modifying core controller logic.
type ChartSource interface {
	// Type returns the source type identifier ("http" or "oci").
	// This is used for source registration and lookup in the registry.
	Type() string

	// ParseAndValidate parses and validates source-specific configuration from raw JSON.
	// This is called per reconciliation with fresh config from Component.Spec.Config.
	//
	// The source should:
	//   - Unmarshal rawConfig into its configuration struct
	//   - Validate all required fields
	//   - Store the validated config internally for subsequent GetChart calls
	//   - Return error if validation fails
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - rawConfig: Raw JSON from Component.Spec.Config (the entire config, not just source section)
	//
	// Returns error if parsing or validation fails.
	ParseAndValidate(ctx context.Context, rawConfig json.RawMessage) error

	// GetChart retrieves a Helm chart using the configuration from ParseAndValidate.
	// Must be called after ParseAndValidate has successfully configured the source.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - settings: Helm CLI settings for chart operations
	//
	// Returns the loaded chart ready for installation/upgrade, or error if retrieval fails.
	GetChart(ctx context.Context, settings *cli.EnvSettings) (*chart.Chart, error)

	// GetVersion returns the configured chart version from the last ParseAndValidate call.
	// This is used for status reporting and change detection.
	//
	// Returns empty string if ParseAndValidate has not been called or version is not available.
	GetVersion() string
}
