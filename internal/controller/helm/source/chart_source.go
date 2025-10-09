// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package source

import (
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
)

// ChartSource provides an abstraction for retrieving Helm charts from different sources.
// Implementations include HTTP repositories (HTTPChartSource) and OCI registries (future).
type ChartSource interface {
	// GetChart retrieves a Helm chart ready for installation or upgrade.
	//
	// Parameters:
	//   - repoName: Repository name (e.g., "bitnami")
	//   - repoURL: Repository URL (e.g., "https://charts.bitnami.com/bitnami" or "oci://registry/repo")
	//   - chartName: Chart name (e.g., "postgresql")
	//   - version: Chart version (e.g., "12.1.2")
	//   - settings: Helm CLI settings (used for chart locating)
	//
	// Returns the loaded chart ready for installation/upgrade.
	GetChart(repoName, repoURL, chartName, version string, settings *cli.EnvSettings) (*chart.Chart, error)
}
