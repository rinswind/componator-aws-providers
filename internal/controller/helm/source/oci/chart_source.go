// Copyright 2025.package oci

// SPDX-License-Identifier: Apache-2.0

package oci

import (
	"context"
	"fmt"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ChartSource provides OCI registry chart retrieval with authentication support.
// Each instance is configured with a specific chart reference and authentication.
type ChartSource struct {
	chartRef     string // Full OCI reference: oci://registry/path:version
	secretRef    *SecretRef
	k8sClient    client.Client
	actionConfig *action.Configuration // Helm action configuration for chart operations
}

// SecretRef references a Kubernetes secret for credentials.
type SecretRef struct {
	Name      string
	Namespace string
}

// NewChartSource creates a new OCI chart source.
// Authentication is optional - if secretRef is nil, assumes public registry.
// Credentials must be in the exact namespace specified in secretRef - no fallback behavior.
func NewChartSource(chartRef string, secretRef *SecretRef, k8sClient client.Client, actionConfig *action.Configuration) *ChartSource {
	return &ChartSource{
		chartRef:     chartRef,
		secretRef:    secretRef,
		k8sClient:    k8sClient,
		actionConfig: actionConfig,
	}
}

// GetChart retrieves a chart from an OCI registry.
// Authenticates if credentials are configured, then pulls the chart using Helm's action.Pull.
func (s *ChartSource) GetChart(ctx context.Context, settings *cli.EnvSettings) (*chart.Chart, error) {
	log := logf.FromContext(ctx)

	log.Info("Fetching chart from OCI registry", "ref", s.chartRef)

	// Parse OCI reference to extract registry host for authentication
	registryHost, chartPath, version, err := parseOCIReference(s.chartRef)
	if err != nil {
		return nil, fmt.Errorf("invalid OCI reference: %w", err)
	}

	log.V(1).Info("Parsed OCI reference",
		"registry", registryHost,
		"chartPath", chartPath,
		"version", version)

	// Create registry client for authentication
	registryClient, err := registry.NewClient(
		registry.ClientOptDebug(settings.Debug),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	// Authenticate if credentials are configured
	if s.secretRef != nil {
		username, password, token, err := s.resolveCredentials(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credentials: %w", err)
		}

		// Login to registry
		if token != "" {
			log.V(1).Info("Authenticating with token", "registry", registryHost)
			if err := registryClient.Login(registryHost, registry.LoginOptBasicAuth("", token)); err != nil {
				return nil, fmt.Errorf("failed to authenticate with token: %w", err)
			}
		} else {
			log.V(1).Info("Authenticating with username/password", "registry", registryHost, "username", username)
			if err := registryClient.Login(registryHost, registry.LoginOptBasicAuth(username, password)); err != nil {
				return nil, fmt.Errorf("failed to authenticate with credentials: %w", err)
			}
		}

		log.Info("Successfully authenticated to registry", "registry", registryHost)
	}

	// Set up temporary directory for chart download
	tempDir, err := os.MkdirTemp("", "helm-oci-chart-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Use Helm's Pull action to download the chart
	pullAction := action.NewPull()
	pullAction.Settings = settings
	pullAction.DestDir = tempDir

	log.Info("Pulling chart from registry", "ref", s.chartRef, "dest", tempDir)

	downloadedPath, err := pullAction.Run(s.chartRef)
	if err != nil {
		return nil, fmt.Errorf("failed to pull chart from OCI registry: %w", err)
	}

	log.V(1).Info("Chart downloaded", "path", downloadedPath)

	// Load the chart from the downloaded file
	loadedChart, err := loader.Load(downloadedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from %s: %w", downloadedPath, err)
	}

	log.Info("Successfully loaded chart from OCI registry",
		"name", loadedChart.Metadata.Name,
		"version", loadedChart.Metadata.Version)

	return loadedChart, nil
}

// resolveCredentials resolves registry credentials from the Kubernetes secret specified in secretRef.
// Fails fast if secret is not found - no fallback behavior.
// Returns: username, password, token (token takes precedence if present)
func (s *ChartSource) resolveCredentials(ctx context.Context) (username, password, token string, err error) {
	log := logf.FromContext(ctx)

	// Get credentials from the specified namespace only - fail fast if not found
	username, password, token, err = s.getSecretCredentials(ctx, s.secretRef.Name, s.secretRef.Namespace)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get credentials from secret %s/%s: %w",
			s.secretRef.Namespace, s.secretRef.Name, err)
	}

	log.V(1).Info("Found credentials",
		"namespace", s.secretRef.Namespace,
		"secret", s.secretRef.Name)

	return username, password, token, nil
}

// getSecretCredentials retrieves credentials from a specific secret.
// Supports both username/password and token-based authentication.
// Secret format:
//   - username/password: keys "username" and "password"
//   - token: key "token"
func (s *ChartSource) getSecretCredentials(ctx context.Context, name, namespace string) (username, password, token string, err error) {
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

// parseOCIReference parses an OCI chart reference into components.
// Input format: oci://registry.example.com/path/to/chart:version
// Returns: registry, chartPath, version
func parseOCIReference(ref string) (registry, chartPath, version string, err error) {
	if !strings.HasPrefix(ref, "oci://") {
		return "", "", "", fmt.Errorf("reference must start with oci://")
	}

	// Remove oci:// prefix
	remainder := ref[6:]

	// Split on : to separate version
	parts := strings.Split(remainder, ":")
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("reference must contain version (format: oci://registry/path:version)")
	}

	pathPart := parts[0]
	version = parts[1]

	// Split path to extract registry (first component)
	pathComponents := strings.Split(pathPart, "/")
	if len(pathComponents) < 2 {
		return "", "", "", fmt.Errorf("reference must contain registry and chart path")
	}

	registry = pathComponents[0]
	chartPath = strings.Join(pathComponents[1:], "/")

	return registry, chartPath, version, nil
}
