// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
)

// ChartSource wraps the shared HTTP CachingRepository singleton to implement the simplified source interface.
// This enables per-chart configuration while preserving singleton benefits (caching, index management).
type ChartSource struct {
	client    *CachingRepository // Shared singleton
	repoName  string             // Repository name for this chart
	repoURL   string             // Repository URL for this chart
	chartName string             // Chart name
	version   string             // Chart version
}

// NewChartSource creates an HTTP chart source adapter wrapping the shared singleton.
func NewChartSource(client *CachingRepository, repoName, repoURL, chartName, version string) *ChartSource {
	return &ChartSource{
		client:    client,
		repoName:  repoName,
		repoURL:   repoURL,
		chartName: chartName,
		version:   version,
	}
}

// GetChart retrieves the chart using stored parameters and the shared HTTP client.
// Context is currently unused by HTTP source but maintained for interface compatibility.
func (s *ChartSource) GetChart(ctx context.Context, settings *cli.EnvSettings) (*chart.Chart, error) {
	// HTTP chart source doesn't currently use context, but we maintain the signature
	// for interface compatibility with OCI source which needs it for authentication
	return s.client.GetChart(s.repoName, s.repoURL, s.chartName, s.version, settings)
}
