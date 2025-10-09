// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"context"
	"errors"
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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	helmRepositoriesFile = "repositories.yaml"
	helmRepositoriesLock = "repositories.lock"
	helmCacheDir         = "repository"
)

// ChartSource provides complete HTTP repository abstraction with caching.
// It encapsulates all repository operations including index caching, repository
// configuration, and chart downloading.
//
// This is designed as a singleton shared across all reconciliation loops.
type ChartSource struct {
	indexCache      *IndexCache
	basePath        string
	repoConfigPath  string
	repoCachePath   string
	refreshInterval time.Duration
}

// NewChartSource creates a new HTTP ChartSource with the specified configuration.
//
// Parameters:
//   - basePath: Base directory for Helm operations (repositories.yaml and cache)
//   - cacheSize: Maximum number of repository indexes to cache (0 = disabled)
//   - cacheTTL: Time-to-live for cached indexes
//   - refreshInterval: How often to check for stale repository indexes on disk
//
// The basePath directory will be created if it doesn't exist.
func NewChartSource(basePath string, cacheSize int, cacheTTL, refreshInterval time.Duration) (*ChartSource, error) {
	log := logf.Log.WithName("http-chart-source")

	// Resolve to absolute path
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for helm directory: %w", err)
	}

	// Ensure cache directory exists
	repoCachePath := filepath.Join(absPath, helmCacheDir)
	if err := os.MkdirAll(repoCachePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create helm cache directory: %w", err)
	}

	s := &ChartSource{
		indexCache:      NewIndexCache(cacheSize, cacheTTL),
		basePath:        absPath,
		repoConfigPath:  filepath.Join(absPath, helmRepositoriesFile),
		repoCachePath:   repoCachePath,
		refreshInterval: refreshInterval,
	}

	log.Info("HTTP chart source initialized",
		"basePath", absPath,
		"cacheSize", cacheSize,
		"cacheTTL", cacheTTL,
		"refreshInterval", refreshInterval)

	return s, nil
}

// GetChart retrieves a Helm chart from an HTTP repository with caching.
// This is the main entry point for chart operations and orchestrates:
//  1. Repository configuration (repositories.yaml)
//  2. Index caching (in-memory and on-disk)
//  3. Chart downloading
//
// Parameters:
//   - repoName: Repository name (e.g., "bitnami")
//   - repoURL: Repository URL (e.g., "https://charts.bitnami.com/bitnami")
//   - chartName: Chart name (e.g., "postgresql")
//   - version: Chart version (e.g., "12.1.2")
//   - settings: Helm CLI settings (used for chart locating)
//
// Returns the loaded chart ready for installation/upgrade.
func (s *ChartSource) GetChart(repoName, repoURL, chartName, version string, settings *cli.EnvSettings) (*chart.Chart, error) {
	log := logf.Log.WithName("http-chart-source")

	// Step 1: Ensure repository is configured in repositories.yaml
	if err := s.ensureRepository(repoName, repoURL); err != nil {
		return nil, fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Step 2: Check in-memory cache for repository index
	if index, found := s.indexCache.Get(repoName); found {
		log.V(1).Info("Using cached index", "repo", repoName)
		return s.loadChartFromIndex(index, chartName, version, settings)
	}

	log.V(1).Info("Index cache miss", "repo", repoName)

	// Step 3: Load or download index from disk/network
	index, err := s.loadOrDownloadIndex(repoName, repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to load repository index: %w", err)
	}

	// Step 4: Cache the index for next time
	s.indexCache.Set(repoName, repoURL, index)

	// Step 5: Load chart from the index
	return s.loadChartFromIndex(index, chartName, version, settings)
}

// ensureRepository ensures a Helm repository is properly configured in repositories.yaml.
// This follows the pattern used by the Helm CLI in pkg/cmd/repo_add.go.
//
// File locking: MANDATORY for horizontally scaled controllers sharing PVC.
// Multiple controller pods writing to the same repositories.yaml WILL corrupt it
// without proper locking. We use github.com/gofrs/flock with 30s timeout following
// the exact pattern from Helm CLI.
func (s *ChartSource) ensureRepository(repoName, repoURL string) error {
	log := logf.Log.WithName("http-chart-source")

	// Acquire file lock for process synchronization (critical for horizontally scaled controllers)
	lockPath := filepath.Join(filepath.Dir(s.repoConfigPath), helmRepositoriesLock)
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
	if err := os.MkdirAll(filepath.Dir(s.repoConfigPath), 0755); err != nil {
		return fmt.Errorf("failed to create repository config directory: %w", err)
	}

	// Load existing repository configuration
	f, err := repo.LoadFile(s.repoConfigPath)
	if err != nil {
		// LoadFile wraps errors, so we need to unwrap to check for os.IsNotExist
		if !errors.Is(err, os.ErrNotExist) {
			// File exists but couldn't be loaded (corrupted, permissions, etc.)
			return fmt.Errorf("load repository file: %w", err)
		}

		// File doesn't exist yet, create new
		f = repo.NewFile()
	}

	// Create repository entry
	entry := &repo.Entry{
		Name: repoName,
		URL:  repoURL,
	}

	// Update repository list (idempotent operation)
	f.Update(entry)

	// Write back (protected by file lock)
	if err := f.WriteFile(s.repoConfigPath, 0644); err != nil {
		return fmt.Errorf("write repository file: %w", err)
	}

	log.V(1).Info("Repository configuration updated", "repo", repoName, "url", repoURL)
	return nil
}

// loadOrDownloadIndex loads a repository index from disk or downloads it if stale.
// This implements a disk-based cache layer below the in-memory cache.
func (s *ChartSource) loadOrDownloadIndex(repoName, repoURL string) (*repo.IndexFile, error) {
	log := logf.Log.WithName("http-chart-source")

	indexPath := filepath.Join(s.repoCachePath, fmt.Sprintf("%s-index.yaml", repoName))

	// Check if index exists and is fresh
	needsDownload := true
	if stat, err := os.Stat(indexPath); err == nil {
		age := time.Since(stat.ModTime())
		if age < s.refreshInterval {
			needsDownload = false
			log.V(1).Info("Using cached index file", "repo", repoName, "age", age)
		} else {
			log.V(1).Info("Index file is stale", "repo", repoName, "age", age, "threshold", s.refreshInterval)
		}
	} else {
		log.V(1).Info("Index file not found, will download", "repo", repoName)
	}

	// Download if needed
	if needsDownload {
		entry := &repo.Entry{
			Name: repoName,
			URL:  repoURL,
		}

		r, err := repo.NewChartRepository(entry, getter.All(cli.New()))
		if err != nil {
			return nil, fmt.Errorf("create chart repository: %w", err)
		}
		r.CachePath = s.repoCachePath

		log.Info("Downloading repository index", "repo", repoName, "url", repoURL)
		if _, err := r.DownloadIndexFile(); err != nil {
			return nil, fmt.Errorf("download repository index: %w", err)
		}
	}

	// Load the index file
	index, err := repo.LoadIndexFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("load index file: %w", err)
	}

	log.V(1).Info("Loaded repository index", "repo", repoName, "chartCount", len(index.Entries))
	return index, nil
}

// loadChartFromIndex locates and loads a chart using the repository index.
// This searches the index for the chart version, downloads it, and loads it.
func (s *ChartSource) loadChartFromIndex(index *repo.IndexFile, chartName, version string, settings *cli.EnvSettings) (*chart.Chart, error) {
	log := logf.Log.WithName("http-chart-source")

	// Configure Helm settings to use our repository paths
	settings.RepositoryConfig = s.repoConfigPath
	settings.RepositoryCache = s.repoCachePath

	// Use Helm's standard chart resolution
	// Note: We can't use the repo/chart format here because we're working with
	// the index directly. Instead, we need to download the chart manually.

	// Find the chart version in the index
	chartVersions, ok := index.Entries[chartName]
	if !ok {
		return nil, fmt.Errorf("chart %q not found in repository index", chartName)
	}

	var chartVersion *repo.ChartVersion
	for _, cv := range chartVersions {
		if cv.Version == version {
			chartVersion = cv
			break
		}
	}

	if chartVersion == nil {
		return nil, fmt.Errorf("chart %q version %q not found in repository index", chartName, version)
	}

	// Use Helm's chart location logic
	// This handles downloading the chart to cache if needed
	chartPathOptions := &action.ChartPathOptions{
		Version: version,
	}

	// Build chart reference from first URL in the chart version
	if len(chartVersion.URLs) == 0 {
		return nil, fmt.Errorf("chart %q version %q has no URLs", chartName, version)
	}

	// Use LocateChart to download and cache the chart
	chartPath, err := chartPathOptions.LocateChart(chartVersion.URLs[0], settings)
	if err != nil {
		return nil, fmt.Errorf("failed to locate chart: %w", err)
	}

	log.V(1).Info("Located chart", "chart", chartName, "version", version, "path", chartPath)

	// Load the chart
	chartLoaded, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from %s: %w", chartPath, err)
	}

	return chartLoaded, nil
}
