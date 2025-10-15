// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// Package oci implements OCI registry chart source for the plugin architecture.
// This source is created per-reconciliation by the Factory with immutable configuration.
package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources"
	"github.com/rinswind/deployment-operator-handlers/internal/controller/helm/sources/filelock"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Factory implements the ChartSourceFactory interface for OCI registries.
// This is a stateless singleton that creates OCISource instances with validated configuration.
type Factory struct {
	k8sClient       client.Client // Kubernetes client for secret resolution (thread-safe)
	repositoryCache string        // Path to Helm repository cache directory
	lockTimeout     time.Duration // Timeout for file locks
}

// NewFactory creates a new OCI chart source factory with Kubernetes client for secret resolution
// and repository cache path.
//
// Parameters:
//   - k8sClient: Kubernetes client for resolving registry credentials from secrets
//   - repositoryCache: Path to Helm repository cache directory (e.g., "/helm/repository")
//   - lockTimeout: Maximum time to wait for file locks
func NewFactory(k8sClient client.Client, repositoryCache string, lockTimeout time.Duration) *Factory {
	return &Factory{
		k8sClient:       k8sClient,
		repositoryCache: repositoryCache,
		lockTimeout:     lockTimeout,
	}
}

// Type returns the source type identifier for registry lookup.
func (f *Factory) Type() string {
	return "oci"
}

// CreateSource parses and validates OCI source configuration, then creates an immutable OCISource instance.
// The rawConfig parameter contains only the source section (already extracted by composite Registry).
// Validates OCI-specific fields including OCI reference format.
//
// Expected JSON structure:
//
//	{
//	  "type": "oci",
//	  "chart": "oci://registry.example.com/path/chart:version",
//	  "authentication": {
//	    "method": "registry",
//	    "secretRef": {"name": "...", "namespace": "..."}
//	  }
//	}
func (f *Factory) CreateSource(ctx context.Context, rawConfig json.RawMessage, settings *cli.EnvSettings) (sources.ChartSource, error) {
	// Parse source configuration (rawConfig is already the source section)
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse OCI source configuration: %w", err)
	}

	// Validate configuration using shared validator instance (with custom OCI reference validator)
	if err := ociSourceValidator.Struct(&config); err != nil {
		return nil, fmt.Errorf("OCI source validation failed: %w", err)
	}

	// Create immutable source instance with validated configuration
	return OCISource{
		k8sClient:       f.k8sClient,
		repositoryCache: f.repositoryCache,
		lockTimeout:     f.lockTimeout,
		config:          config,
		settings:        settings,
	}, nil
}

// OCISource implements the ChartSource interface for OCI registries.
// This is a per-reconciliation instance created by Factory with immutable configuration.
// All configuration is baked in at creation time, making it thread-safe.
type OCISource struct {
	k8sClient       client.Client    // Kubernetes client for secret resolution (thread-safe)
	repositoryCache string           // Path to Helm repository cache directory
	lockTimeout     time.Duration    // Timeout for file locks
	config          Config           // Immutable configuration (no pointer, value type)
	settings        *cli.EnvSettings // Immutable settings (no mutation after creation)
}

// LocateChart retrieves a chart from an OCI registry and returns the path to the
// cached chart file.
//
// This handles:
//   - Optional authentication via Kubernetes secrets
//   - Chart downloading using ChartDownloader with RegistryClient
//   - Returns path to cached chart archive
func (s OCISource) LocateChart(ctx context.Context) (string, error) {

	// Parse OCI reference to extract registry host for authentication
	registryHost, chartPath, version, err := parseOCIReference(s.config.Chart)
	if err != nil {
		return "", fmt.Errorf("invalid OCI reference: %w", err)
	}

	log := logf.FromContext(ctx).WithValues(
		"registry", registryHost,
		"chart", chartPath,
		"version", version)

	log.Info("Fetching chart from OCI registry")
	log.V(1).Info("Parsed OCI reference")

	// Create registry client for authentication
	registryClient, err := registry.NewClient(
		registry.ClientOptDebug(s.settings.Debug),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create registry client: %w", err)
	}

	// Authenticate if credentials are configured
	if s.config.Authentication != nil {
		username, password, token, err := s.resolveCredentials(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to resolve credentials: %w", err)
		}

		// Login to registry
		if token != "" {
			log.V(1).Info("Authenticating with token")
			if err := registryClient.Login(registryHost, registry.LoginOptBasicAuth("", token)); err != nil {
				return "", fmt.Errorf("failed to authenticate with token: %w", err)
			}
		} else {
			log.V(1).Info("Authenticating with username/password", "username", username)
			if err := registryClient.Login(registryHost, registry.LoginOptBasicAuth(username, password)); err != nil {
				return "", fmt.Errorf("failed to authenticate with credentials: %w", err)
			}
		}

		log.Info("Successfully authenticated to registry")
	}

	// Create lock file path in the same directory as the chart tarball
	safePath := strings.ReplaceAll(chartPath, "/", "-")
	lockPath := filepath.Join(s.repositoryCache, fmt.Sprintf("%s-%s-%s.lock", registryHost, safePath, version))

	// Protect chart download with file lock
	var downloadedPath string
	err = filelock.WithLock(lockPath, s.lockTimeout, func() error {
		// Use ChartDownloader to download chart to cache with authenticated registry client
		dl := &downloader.ChartDownloader{
			Out:            os.Stdout,
			RegistryClient: registryClient,
			Getters:        getter.All(s.settings),
			Options:        []getter.Option{getter.WithRegistryClient(registryClient)},
		}

		// Download to cache - version is empty string for OCI (embedded in ref)
		var err error
		downloadedPath, _, err = dl.DownloadTo(s.config.Chart, "", s.repositoryCache)
		if err != nil {
			log.Error(err, "Chart download failed")
			return fmt.Errorf("failed to download chart from OCI registry: %w", err)
		}

		log.Info("Downloaded chart successfully", "path", downloadedPath)
		return nil
	})
	if err != nil {
		return "", err
	}

	return downloadedPath, nil
}

// GetVersion returns the configured chart version.
// For OCI sources, this extracts the version from the OCI reference.
func (s OCISource) GetVersion() string {
	// Extract version from OCI reference
	_, _, version, err := parseOCIReference(s.config.Chart)
	if err != nil {
		return ""
	}
	return version
}

// resolveCredentials resolves registry credentials from the Kubernetes secret specified in config.
// Fails fast if secret is not found - no fallback behavior.
// Returns: username, password, token (token takes precedence if present)
func (s OCISource) resolveCredentials(ctx context.Context) (username, password, token string, err error) {
	secretRef := s.config.Authentication.SecretRef
	log := logf.FromContext(ctx).WithValues(
		"secretNamespace", secretRef.Namespace,
		"secretName", secretRef.Name)

	// Get credentials from the specified namespace only - fail fast if not found
	username, password, token, err = s.getSecretCredentials(ctx, secretRef.Name, secretRef.Namespace)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get credentials from secret %s/%s: %w",
			secretRef.Namespace, secretRef.Name, err)
	}

	log.V(1).Info("Found credentials")

	return username, password, token, nil
}

// getSecretCredentials retrieves credentials from a specific secret.
// Supports both username/password and token-based authentication.
// Secret format:
//   - username/password: keys "username" and "password"
//   - token: key "token"
func (s OCISource) getSecretCredentials(ctx context.Context, name, namespace string) (username, password, token string, err error) {
	var secret corev1.Secret

	secretKey := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	if err := s.k8sClient.Get(ctx, secretKey, &secret); err != nil {
		return "", "", "", fmt.Errorf("failed to get secret: %w", err)
	}

	// Check for token first (takes precedence)
	if tokenBytes, ok := secret.Data["token"]; ok && len(tokenBytes) > 0 {
		return "", "", string(tokenBytes), nil
	}

	// Check for username/password
	usernameBytes, hasUsername := secret.Data["username"]
	passwordBytes, hasPassword := secret.Data["password"]

	if hasUsername && hasPassword {
		return string(usernameBytes), string(passwordBytes), "", nil
	}

	return "", "", "", fmt.Errorf("secret must contain either 'token' or both 'username' and 'password' keys")
}
