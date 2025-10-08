// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"fmt"
	"os"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
)

// loadHelmChart handles the common chart preparation steps for both install and upgrade
// Returns the loaded chart ready for deployment
func loadHelmChart(config *HelmConfig, settings *cli.EnvSettings) (*chart.Chart, error) {
	// Use Helm's standard chart resolution with repo/chart format
	chartRef := fmt.Sprintf("%s/%s", config.Repository.Name, config.Chart.Name)
	chartPathOptions := &action.ChartPathOptions{}

	cp, err := chartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart %s: %w", chartRef, err)
	}

	chart, err := loader.Load(cp)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from %s: %w", cp, err)
	}

	return chart, nil
}

// setupHelmRepository configures a Helm repository properly for ephemeral containers
// This creates the necessary repository configuration files that Helm expects
// Returns a cleanup function that should be called with defer
func setupHelmRepository(config *HelmConfig, settings *cli.EnvSettings) (*repo.ChartRepository, func(), error) {
	// Create temporary directories for Helm configuration
	tempConfigDir, err := os.MkdirTemp("", "helm-config-")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temporary config directory: %w", err)
	}

	tempCacheDir, err := os.MkdirTemp("", "helm-cache-")
	if err != nil {
		os.RemoveAll(tempConfigDir)
		return nil, nil, fmt.Errorf("failed to create temporary cache directory: %w", err)
	}

	// Cleanup function to remove temporary directories
	cleanup := func() {
		os.RemoveAll(tempConfigDir)
		os.RemoveAll(tempCacheDir)
	}

	// Configure Helm settings to use our temporary directories
	settings.RepositoryConfig = tempConfigDir + "/repositories.yaml"
	settings.RepositoryCache = tempCacheDir

	// Load or create repository file
	repoFile := repo.NewFile()

	// Create repository entry
	repoEntry := &repo.Entry{
		Name: config.Repository.Name,
		URL:  config.Repository.URL,
	}

	// Create chart repository instance for index download
	chartRepo, err := repo.NewChartRepository(repoEntry, getter.All(settings))
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to create chart repository: %w", err)
	}

	// Set the cache path
	chartRepo.CachePath = settings.RepositoryCache

	// Download the repository index - this validates the repo and caches the index
	_, err = chartRepo.DownloadIndexFile()
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to download repository index: %w", err)
	}

	// Add repository to the configuration file
	repoFile.Update(repoEntry)

	// Write the repository configuration file
	if err := repoFile.WriteFile(settings.RepositoryConfig, 0644); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to write repository configuration: %w", err)
	}

	// Return the repository and cleanup function
	// Caller must defer cleanup() to ensure temporary directories are removed
	return chartRepo, cleanup, nil
}
