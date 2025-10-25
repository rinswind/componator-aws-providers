// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

package http

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rinswind/componator-providers/internal/controller/helm/sources/filelock"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	helmRepositoriesFile = "repositories.yaml"
	helmRepositoriesLock = "repositories.lock"
	helmCacheDir         = "repository"
)

// CachingRepository provides complete HTTP repository abstraction with caching.
// It encapsulates all repository operations including index caching, repository
// configuration, and chart downloading.
//
// This is designed as a singleton shared across all reconciliation loops.
type CachingRepository struct {
	indexCache      *IndexCache
	basePath        string
	repoConfigPath  string
	repoCachePath   string
	refreshInterval time.Duration
	lockTimeout     time.Duration
}

// NewCachingRepository creates a new HTTP CachingRepository with the specified configuration.
//
// Parameters:
//   - basePath: Base directory for Helm operations (repositories.yaml and cache)
//   - cacheSize: Maximum number of repository indexes to cache (0 = disabled)
//   - cacheTTL: Time-to-live for cached indexes
//   - refreshInterval: How often to check for stale repository indexes on disk
//   - lockTimeout: Maximum time to wait for file locks
//
// The basePath directory will be created if it doesn't exist.
func NewCachingRepository(basePath string, cacheSize int, cacheTTL, refreshInterval, lockTimeout time.Duration) (*CachingRepository, error) {
	log := logf.Log.WithName("http-caching-repository")

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

	s := &CachingRepository{
		indexCache:      NewIndexCache(cacheSize, cacheTTL),
		basePath:        absPath,
		repoConfigPath:  filepath.Join(absPath, helmRepositoriesFile),
		repoCachePath:   repoCachePath,
		refreshInterval: refreshInterval,
		lockTimeout:     lockTimeout,
	}

	log.Info("HTTP chart source initialized",
		"basePath", absPath,
		"cacheSize", cacheSize,
		"cacheTTL", cacheTTL,
		"refreshInterval", refreshInterval,
		"lockTimeout", lockTimeout)

	return s, nil
}

// LocateChart retrieves a Helm chart from an HTTP repository with caching and returns
// the path to the cached chart file.
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
//   - settings: Helm CLI settings (used for chart downloading)
//
// Returns the path to the downloaded chart file (typically a .tgz in Helm cache).
func (s *CachingRepository) LocateChart(repoName, repoURL, chartName, version string, settings *cli.EnvSettings) (string, error) {
	log := logf.Log.WithName("http-chart-source").WithValues(
		"repo", repoName,
		"chart", chartName,
		"version", version)

	// Step 1: Ensure repository is configured in repositories.yaml
	if err := s.ensureRepository(repoName, repoURL); err != nil {
		return "", fmt.Errorf("failed to ensure repository: %w", err)
	}

	// Step 2: Check in-memory cache for repository index
	if index, found := s.indexCache.Get(repoName); found {
		log.V(1).Info("Using cached index")
		return s.loadChartFromIndex(repoName, repoURL, index, chartName, version, settings)
	}

	log.V(1).Info("Index cache miss")

	// Step 3: Load or download index from disk/network
	index, err := s.loadOrDownloadIndex(repoName, repoURL)
	if err != nil {
		return "", fmt.Errorf("failed to load repository index: %w", err)
	}

	// Step 4: Cache the index for next time
	s.indexCache.Set(repoName, repoURL, index)

	// Step 5: Load chart from the index
	return s.loadChartFromIndex(repoName, repoURL, index, chartName, version, settings)
}

// ensureRepository ensures a Helm repository is properly configured in repositories.yaml.
// This follows the pattern used by the Helm CLI in pkg/cmd/repo_add.go.
//
// File locking: MANDATORY for horizontally scaled controllers sharing PVC.
// Multiple controller pods writing to the same repositories.yaml WILL corrupt it
// without proper locking. We use filelock with 30s timeout following
// the exact pattern from Helm CLI.
func (s *CachingRepository) ensureRepository(repoName, repoURL string) error {
	log := logf.Log.WithName("http-chart-source").WithValues("repo", repoName, "url", repoURL)

	// Acquire file lock for process synchronization (critical for horizontally scaled controllers)
	lockPath := filepath.Join(filepath.Dir(s.repoConfigPath), helmRepositoriesLock)

	return filelock.WithLock(lockPath, s.lockTimeout, func() error {
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

		log.V(1).Info("Repository configuration updated")
		return nil
	})
}

// loadOrDownloadIndex loads a repository index from disk or downloads it if stale.
// This implements a disk-based cache layer below the in-memory cache.
// Uses file locking to prevent concurrent downloads of the same index.
func (s *CachingRepository) loadOrDownloadIndex(repoName, repoURL string) (*repo.IndexFile, error) {
	log := logf.Log.WithName("http-chart-source").WithValues("repo", repoName)

	indexPath := filepath.Join(s.repoCachePath, fmt.Sprintf("%s-index.yaml", repoName))
	lockPath := filepath.Join(s.repoCachePath, fmt.Sprintf("%s-index.lock", repoName))

	// Protect index download with file lock
	err := filelock.WithLock(lockPath, s.lockTimeout, func() error {
		// Check if index exists and is fresh (inside lock to avoid race)
		needsDownload := true
		if stat, err := os.Stat(indexPath); err == nil {
			age := time.Since(stat.ModTime())
			if age < s.refreshInterval {
				needsDownload = false
				log.V(1).Info("Using cached index file", "age", age)
			} else {
				log.V(1).Info("Index file is stale", "age", age, "threshold", s.refreshInterval)
			}
		} else {
			log.V(1).Info("Index file not found, will download")
		}

		// Download if needed
		if needsDownload {
			entry := &repo.Entry{
				Name: repoName,
				URL:  repoURL,
			}

			r, err := repo.NewChartRepository(entry, getter.All(cli.New()))
			if err != nil {
				return fmt.Errorf("create chart repository: %w", err)
			}
			r.CachePath = s.repoCachePath

			log.Info("Downloading repository index", "url", repoURL)
			if _, err := r.DownloadIndexFile(); err != nil {
				return fmt.Errorf("download repository index: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Load the index file (outside lock - read-only operation)
	index, err := repo.LoadIndexFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("load index file: %w", err)
	}

	log.V(1).Info("Loaded repository index", "chartCount", len(index.Entries))
	return index, nil
}

// loadChartFromIndex locates and downloads a chart using the repository index.
// This searches the index for the chart version and downloads it to Helm cache
// using ChartDownloader. Uses file locking to prevent concurrent downloads of the same chart.
func (s *CachingRepository) loadChartFromIndex(
	repoName, repoURL string,
	index *repo.IndexFile,
	chartName,
	chartVersion string,
	settings *cli.EnvSettings) (string, error) {

	log := logf.Log.WithName("http-chart-source").WithValues("chart", chartName, "version", chartVersion)

	// Configure Helm settings to use our repository paths
	settings.RepositoryConfig = s.repoConfigPath
	settings.RepositoryCache = s.repoCachePath

	// Find the chart version in the index
	chartVersions, ok := index.Entries[chartName]
	if !ok {
		return "", fmt.Errorf("chart %q not found in repository index", chartName)
	}

	var resolvedChartVersion *repo.ChartVersion
	for _, cv := range chartVersions {
		if cv.Version == chartVersion {
			resolvedChartVersion = cv
			break
		}
	}

	if resolvedChartVersion == nil {
		return "", fmt.Errorf("chart %q version %q not found in repository index", chartName, chartVersion)
	}

	// Get chart URL from first URL in the chart version
	if len(resolvedChartVersion.URLs) == 0 {
		return "", fmt.Errorf("chart %q version %q has no URLs", chartName, chartVersion)
	}
	relativeChartURL := resolvedChartVersion.URLs[0]

	// Resolve relative URL against repository base URL
	chartURL, err := repo.ResolveReferenceURL(repoURL, relativeChartURL)
	if err != nil {
		return "", fmt.Errorf("failed to resolve chart URL: %w", err)
	}

	log = log.WithValues("chartURL", chartURL)

	log.V(1).Info("Resolved chart URL")

	// Create lock file path in the same directory as the chart tarball
	lockPath := filepath.Join(s.repoCachePath, fmt.Sprintf("%s-%s-%s.lock", repoName, chartName, chartVersion))

	// Protect chart download with file lock
	var chartPath string
	err = filelock.WithLock(lockPath, s.lockTimeout, func() error {
		// Use ChartDownloader to download chart to cache
		dl := &downloader.ChartDownloader{
			Out:              os.Stdout,
			RepositoryConfig: s.repoConfigPath,
			RepositoryCache:  s.repoCachePath,
			Getters:          getter.All(settings),
		}

		var err error
		chartPath, _, err = dl.DownloadTo(chartURL, chartVersion, s.repoCachePath)
		if err != nil {
			log.Error(err, "Chart download failed")
			return fmt.Errorf("failed to download chart: %w", err)
		}

		log.Info("Downloaded chart successfully", "path", chartPath)
		return nil
	})
	if err != nil {
		return "", err
	}

	return chartPath, nil
}
