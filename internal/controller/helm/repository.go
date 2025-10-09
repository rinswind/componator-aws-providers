// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
)

const (
	helmRepositoriesFile = "repositories.yaml"
	helmRepositoriesLock = "repositories.lock"
	helmCacheDir         = "repository"
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

// setupHelmRepository configures Helm repository following Helm CLI patterns.
// Uses a single shared repositories.yaml and cache directory at /helm, matching how helm CLI works.
// The /helm directory must be provided by the deployment (either PVC or emptyDir).
// Directory creation is handled once by NewHelmOperationsFactory().
func setupHelmRepository(config *HelmConfig, settings *cli.EnvSettings, indexRefreshInterval time.Duration) (*repo.ChartRepository, func(), error) {
	// Use /helm as the base directory - deployment is responsible for mounting appropriate volume
	helmBaseDir := "/helm"
	repoConfigPath := filepath.Join(helmBaseDir, helmRepositoriesFile)
	repoCachePath := filepath.Join(helmBaseDir, helmCacheDir)

	// Configure Helm settings with shared paths
	settings.RepositoryConfig = repoConfigPath
	settings.RepositoryCache = repoCachePath

	// Add or update repository configuration
	if err := ensureRepository(repoConfigPath, repoCachePath, config.Repository.Name, config.Repository.URL, indexRefreshInterval); err != nil {
		return nil, nil, err
	}

	// Create chart repository object for return
	entry := &repo.Entry{
		Name: config.Repository.Name,
		URL:  config.Repository.URL,
	}
	chartRepo, err := repo.NewChartRepository(entry, getter.All(settings))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create chart repository: %w", err)
	}
	chartRepo.CachePath = repoCachePath

	return chartRepo, func() {}, nil
}

// ensureRepository ensures a Helm repository is properly configured.
// This follows the pattern used by the Helm CLI in pkg/cmd/repo_add.go.
//
// Note: The Helm Go SDK does NOT provide a reusable library function for this.
// All repository management logic is in pkg/cmd/repo_add.go (CLI layer).
// We replicate the pattern using the building blocks from pkg/repo/:
// - repo.LoadFile() / repo.NewFile()
// - repo.File.Update() (idempotent)
// - repo.File.WriteFile() (NOT atomic - uses os.WriteFile directly)
// - repo.NewChartRepository()
// - repo.ChartRepository.DownloadIndexFile() (NOT atomic - uses os.WriteFile directly)
//
// File locking: MANDATORY for horizontally scaled controllers sharing PVC.
// Multiple controller pods writing to the same repositories.yaml WILL corrupt it
// without proper locking. We use github.com/gofrs/flock with 30s timeout following
// the exact pattern from Helm CLI.
//
// Index refresh: Downloads repository index only if it doesn't exist or is older than
// the configured refresh interval. This balances chart version freshness with network efficiency.
// For example, with a 5-minute interval, deploying 100 tenant Components within 5 minutes results
// in only 1 index download instead of 100, while still detecting new chart versions within
// a reasonable timeframe.
func ensureRepository(repoFile, repoCache, name, url string, refreshInterval time.Duration) error {
	// Acquire file lock for process synchronization (critical for horizontally scaled controllers)
	lockPath := filepath.Join(filepath.Dir(repoFile), helmRepositoriesLock)
	fileLock := flock.New(lockPath)
	lockCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	locked, err := fileLock.TryLockContext(lockCtx, time.Second)
	if err == nil && locked {
		defer fileLock.Unlock()
	}
	if err != nil {
		return fmt.Errorf("failed to acquire file lock: %w", err)
	}

	// Ensure parent directory exists before attempting to load the file
	// This ensures repo.LoadFile returns clean os.IsNotExist when file doesn't exist
	if err := os.MkdirAll(filepath.Dir(repoFile), 0755); err != nil {
		return fmt.Errorf("failed to create repository config directory: %w", err)
	}

	// Load existing repository configuration
	f, err := repo.LoadFile(repoFile)
	if err != nil {
		if !os.IsNotExist(err) {
			// File exists but couldn't be loaded (corrupted, permissions, etc.)
			return fmt.Errorf("load repository file: %w", err)
		}
		// File doesn't exist yet, create new
		f = repo.NewFile()
	}
	if f == nil {
		// Shouldn't happen, but be defensive
		f = repo.NewFile()
	}

	// Create repository entry
	entry := &repo.Entry{
		Name: name,
		URL:  url,
	}

	// Update repository list (idempotent operation)
	f.Update(entry)

	// Write back (protected by file lock)
	if err := f.WriteFile(repoFile, 0644); err != nil {
		return fmt.Errorf("write repository file: %w", err)
	}

	// Check if repository index needs refresh
	// Download if: index doesn't exist OR index is older than refresh interval
	indexPath := filepath.Join(repoCache, fmt.Sprintf("%s-index.yaml", name))
	needsDownload := true

	if stat, err := os.Stat(indexPath); err == nil {
		age := time.Since(stat.ModTime())
		if age < refreshInterval {
			needsDownload = false
		}
	}

	if needsDownload {
		// Create chart repository client and download index
		r, err := repo.NewChartRepository(entry, getter.All(cli.New()))
		if err != nil {
			return fmt.Errorf("create chart repository: %w", err)
		}
		r.CachePath = repoCache

		// Download and cache the repository index
		if _, err := r.DownloadIndexFile(); err != nil {
			return fmt.Errorf("download repository index: %w", err)
		}
	}

	return nil
}
