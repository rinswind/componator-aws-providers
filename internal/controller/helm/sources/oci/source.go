// Copyright 2025.
// SPDX-License-Identifier: Apache-2.0

// Package oci implements OCI registry chart source for the plugin architecture.
// This source handles OCI chart retrieval with optional authentication and implements
// per-reconciliation configuration via ParseAndValidate.
package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-playground/validator/v10"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Source implements the ChartSource interface for OCI registries.
// This is a long-lived singleton that stores the Kubernetes client for credential resolution.
// Configuration is provided per reconciliation via ParseAndValidate.
type Source struct {
	k8sClient client.Client // Kubernetes client for secret resolution
	config    *Config       // Current configuration from last ParseAndValidate call
}

// NewSource creates a new OCI chart source with Kubernetes client for secret resolution.
// The source is stateless until ParseAndValidate is called with configuration.
func NewSource(k8sClient client.Client) *Source {
	return &Source{
		k8sClient: k8sClient,
	}
}

// Type returns the source type identifier for registry lookup.
func (s *Source) Type() string {
	return "oci"
}

// ParseAndValidate parses and validates OCI source configuration from raw JSON.
// This extracts the "source" section and validates the OCI-specific fields.
//
// Expected JSON structure:
//
//	{
//	  "releaseName": "...",
//	  "releaseNamespace": "...",
//	  "source": {
//	    "type": "oci",
//	    "chart": "oci://registry.example.com/path/chart:version",
//	    "authentication": {
//	      "method": "registry",
//	      "secretRef": {"name": "...", "namespace": "..."}
//	    }
//	  }
//	}
func (s *Source) ParseAndValidate(ctx context.Context, rawConfig json.RawMessage) error {
	// Parse the raw config to extract the source section
	var configMap map[string]json.RawMessage
	if err := json.Unmarshal(rawConfig, &configMap); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Extract source section
	rawSource, hasSource := configMap["source"]
	if !hasSource {
		return fmt.Errorf("source field is required")
	}

	// Parse source configuration
	var config Config
	if err := json.Unmarshal(rawSource, &config); err != nil {
		return fmt.Errorf("failed to parse OCI source configuration: %w", err)
	}

	// Validate configuration with custom OCI reference validator
	validate := validator.New()
	if err := validate.RegisterValidation("oci_reference", validateOCIReference); err != nil {
		return fmt.Errorf("failed to register OCI validator: %w", err)
	}

	if err := validate.Struct(&config); err != nil {
		return fmt.Errorf("OCI source validation failed: %w", err)
	}

	// Store validated configuration
	s.config = &config

	return nil
}

// LocateChart retrieves a chart from an OCI registry and returns the path to the
// downloaded chart file.
//
// This handles:
//   - Optional authentication via Kubernetes secrets
//   - Chart pulling using Helm's action.Pull
//   - Returns path to downloaded chart archive
//
// Note: This currently uses action.Pull which is a temporary implementation.
// Phase 2 will refactor to use ChartDownloader directly for proper authentication.
func (s *Source) LocateChart(ctx context.Context, settings *cli.EnvSettings) (string, error) {
	if s.config == nil {
		return "", fmt.Errorf("ParseAndValidate must be called before LocateChart")
	}

	log := logf.FromContext(ctx)
	log.Info("Fetching chart from OCI registry", "ref", s.config.Chart)

	// Parse OCI reference to extract registry host for authentication
	registryHost, chartPath, version, err := parseOCIReference(s.config.Chart)
	if err != nil {
		return "", fmt.Errorf("invalid OCI reference: %w", err)
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
			log.V(1).Info("Authenticating with token", "registry", registryHost)
			if err := registryClient.Login(registryHost, registry.LoginOptBasicAuth("", token)); err != nil {
				return "", fmt.Errorf("failed to authenticate with token: %w", err)
			}
		} else {
			log.V(1).Info("Authenticating with username/password", "registry", registryHost, "username", username)
			if err := registryClient.Login(registryHost, registry.LoginOptBasicAuth(username, password)); err != nil {
				return "", fmt.Errorf("failed to authenticate with credentials: %w", err)
			}
		}

		log.Info("Successfully authenticated to registry", "registry", registryHost)
	}

	// Set up temporary directory for chart download
	tempDir, err := os.MkdirTemp("", "helm-oci-chart-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Use Helm's Pull action to download the chart
	pullAction := action.NewPull()
	pullAction.Settings = settings
	pullAction.DestDir = tempDir

	log.Info("Pulling chart from registry", "ref", s.config.Chart, "dest", tempDir)

	downloadedPath, err := pullAction.Run(s.config.Chart)
	if err != nil {
		return "", fmt.Errorf("failed to pull chart from OCI registry: %w", err)
	}

	log.Info("Chart downloaded to path", "path", downloadedPath)

	return downloadedPath, nil
}

// GetVersion returns the configured chart version from the last ParseAndValidate call.
// For OCI sources, this extracts the version from the OCI reference.
func (s *Source) GetVersion() string {
	if s.config == nil {
		return ""
	}

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
func (s *Source) resolveCredentials(ctx context.Context) (username, password, token string, err error) {
	log := logf.FromContext(ctx)

	secretRef := s.config.Authentication.SecretRef

	// Get credentials from the specified namespace only - fail fast if not found
	username, password, token, err = s.getSecretCredentials(ctx, secretRef.Name, secretRef.Namespace)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get credentials from secret %s/%s: %w",
			secretRef.Namespace, secretRef.Name, err)
	}

	log.V(1).Info("Found credentials",
		"namespace", secretRef.Namespace,
		"secret", secretRef.Name)

	return username, password, token, nil
}

// getSecretCredentials retrieves credentials from a specific secret.
// Supports both username/password and token-based authentication.
// Secret format:
//   - username/password: keys "username" and "password"
//   - token: key "token"
func (s *Source) getSecretCredentials(ctx context.Context, name, namespace string) (username, password, token string, err error) {
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
