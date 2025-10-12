// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package source

import (
	"context"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
)

// ChartSource provides an abstraction for retrieving Helm charts from different sources.
// Implementations include HTTPChartSource (HTTP repositories) and OCIChartSource (OCI registries).
//
// Design: Each ChartSource instance is fully configured at construction time with
// addressing parameters (repository, chart, version). This eliminates the need for
// method parameters that differ between source types and enables clean, source-agnostic usage.
type ChartSource interface {
	// GetChart retrieves a Helm chart ready for installation or upgrade.
	// All addressing parameters are provided at construction time.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - settings: Helm CLI settings for chart operations
	//
	// Returns the loaded chart ready for installation/upgrade.
	GetChart(ctx context.Context, settings *cli.EnvSettings) (*chart.Chart, error)
}
